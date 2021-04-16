package prometheus

import (
	"bytes"
	"context"
	"fmt"
	"text/template"
	"time"

	"github.com/prometheus/prometheus/pkg/rulefmt"

	"github.com/slok/sloth/internal/model"
)

// genFunc knows how to generate an SLI recording rule for a specific time window.
type genFunc func(slo SLO, window time.Duration) (*rulefmt.Rule, error)

type sliRecordingRulesGenerator struct {
	genFunc genFunc
}

var SLIRecordingRulesGenerator = sliRecordingRulesGenerator{genFunc: defaultSLIRecordGenerator}

func (s sliRecordingRulesGenerator) GenerateSLIRecordingRules(ctx context.Context, slo SLO, alerts model.MWMBAlertGroup) ([]rulefmt.Rule, error) {
	extraLabels := map[string]string{
		"sloth_slo":     slo.ID,
		"sloth_service": slo.Service,
	}

	// Get the windows we need the recording rules.
	windows := getAlertGroupWindows(alerts)
	windows = append(windows, slo.TimeWindow) // Add the total time window as a handy helper.

	// Generate the rules
	rules := make([]rulefmt.Rule, 0, len(windows))
	for _, window := range windows {
		rule, err := s.genFunc(slo, window)
		if err != nil {
			return nil, fmt.Errorf("could not create %q SLO rule for window %s: %w", slo.ID, window, err)
		}

		// Add extra SLO labels.
		rule.Labels = mergeLabels(rule.Labels, extraLabels)

		rules = append(rules, *rule)
	}

	return rules, nil
}

const (
	tplKeyWindow  = "window"
	sliMetricName = "slo:sli_error:ratio_rate%s"
)

func defaultSLIRecordGenerator(slo SLO, window time.Duration) (*rulefmt.Rule, error) {
	// Generate our first level of template by assembling the error and total expressions.
	sliExprTpl := fmt.Sprintf(`
(%s)
/
(%s)
`, slo.SLI.ErrorQuery, slo.SLI.TotalQuery)

	// Render with our templated data.
	tpl, err := template.New("sliExpr").Option("missingkey=error").Parse(sliExprTpl)
	if err != nil {
		return nil, fmt.Errorf("could not create SLI expression template data: %w", err)
	}

	strWindow := timeDurationToPromStr(window)
	var b bytes.Buffer
	err = tpl.Execute(&b, map[string]string{
		tplKeyWindow: strWindow,
	})
	if err != nil {
		return nil, fmt.Errorf("could not render SLI expression template: %w", err)
	}

	// Add extra labels to rule.
	ruleLabels := mergeLabels(slo.Labels, map[string]string{
		"window": strWindow,
	})

	return &rulefmt.Rule{
		Record: fmt.Sprintf(sliMetricName, strWindow),
		Expr:   b.String(),
		Labels: ruleLabels,
	}, nil
}