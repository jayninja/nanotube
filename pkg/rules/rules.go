// Package rules provides primitives for working with routing rules.
package rules

import (
	"fmt"
	"regexp"

	"go.uber.org/zap"

	"github.com/bookingcom/nanotube/pkg/conf"
	"github.com/bookingcom/nanotube/pkg/metrics"
	"github.com/bookingcom/nanotube/pkg/rec"
	"github.com/bookingcom/nanotube/pkg/target"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

// Rules represent all the routing rules/routing table.
type Rules struct {
	rules        []Rule
	measureRegex bool
	metrics      *metrics.Prom
}

// Rule is a routing rule.
type Rule struct {
	Regexs     []string
	Prefixes   []string
	Targets    []target.ClusterTarget
	Continue   bool
	CompiledRE []*regexp.Regexp

	PrefixTrie *PrefixTrie

	regexDuration []prometheus.Observer
}

// Build reads rules from config, compiles them.
func Build(crs *conf.Rules, clusters target.Clusters, measureRegex bool, ms *metrics.Prom) (Rules, error) {
	var rs Rules

	rs.measureRegex = measureRegex
	rs.metrics = ms

	for _, cr := range crs.Rule {
		r := Rule{
			Regexs:   cr.Regexs,
			Prefixes: cr.Prefixes,
			Continue: cr.Continue,
		}
		for _, clName := range cr.Clusters {
			cl, ok := clusters[clName]
			if !ok {
				return rs,
					fmt.Errorf("got non-existent cluster name %s in the rules config",
						clName)
			}
			r.Targets = append(r.Targets, cl)
		}

		rs.rules = append(rs.rules, r)
	}

	err := rs.compile()
	if err != nil {
		return rs, errors.Wrap(err, "rules compilation failed")
	}

	return rs, nil
}

// compile precompiles regexps for perf and performs validation.
func (rs Rules) compile() error {
	for i := range rs.rules {
		rs.rules[i].CompiledRE = make([]*regexp.Regexp, 0)
		for _, re := range rs.rules[i].Regexs {
			cre, err := regexp.Compile(re)
			if err != nil {
				return errors.Wrapf(err, "compiling regex %s failed", cre)
			}
			rs.rules[i].CompiledRE = append(rs.rules[i].CompiledRE, cre)
			if rs.measureRegex {
				labels := prometheus.Labels{
					"rule_type": "routing",
					"regex":     re,
				}
				rs.rules[i].regexDuration = append(rs.rules[i].regexDuration, rs.metrics.RegexDuration.With(labels))
			}
		}

		rs.rules[i].PrefixTrie = NewPrefixTrie()
		for _, pre := range rs.rules[i].Prefixes {
			rs.rules[i].PrefixTrie.Add([]byte(pre))
		}
	}

	return nil
}

// TestBuild makes a set of rules for testing.
func TestBuild(crs conf.Rules, clusters map[string]*target.TestTarget, measureRegex bool, ms *metrics.Prom) (Rules, error) {
	var rs Rules

	rs.measureRegex = measureRegex
	rs.metrics = ms

	for _, cr := range crs.Rule {
		r := Rule{
			Regexs:   cr.Regexs,
			Prefixes: cr.Prefixes,
			Continue: cr.Continue,
		}
		for _, clName := range cr.Clusters {
			cl, ok := clusters[clName]
			if !ok {
				return rs,
					fmt.Errorf("got non-existent cluster name %s in the rules config",
						clName)
			}
			r.Targets = append(r.Targets, cl)
		}

		rs.rules = append(rs.rules, r)
	}

	err := rs.compile()
	if err != nil {
		return rs, errors.Wrap(err, "rules compilation failed")
	}

	return rs, nil
}

// RouteRecBytes a record by following the rules
func (rs Rules) RouteRecBytes(r *rec.RecBytes, lg *zap.Logger) {
	pushedTo := make(map[target.ClusterTarget]struct{})

	for _, rl := range rs.rules {
		matchedRule := rl.MatchBytes(r, rs.measureRegex)
		if matchedRule {
			for _, cl := range rl.Targets {
				if _, pushedBefore := pushedTo[cl]; pushedBefore {
					continue
				}
				err := cl.PushBytes(r, rs.metrics)
				if err != nil {
					lg.Error("push to cluster failed",
						zap.Error(err),
						zap.String("cluster", cl.GetName()),
						zap.String("record", string(r.Serialize())))
				}
				pushedTo[cl] = struct{}{}
			}
		}

		if matchedRule && !rl.Continue {
			break
		}
	}
}

// MatchBytes a record with any of the rule regexps
func (rl Rule) MatchBytes(r *rec.RecBytes, measureRegex bool) bool {
	if rl.PrefixTrie.Check(r.Path) {
		return true
	}

	var timer *prometheus.Timer

	for idx, re := range rl.CompiledRE {
		if measureRegex {
			timer = prometheus.NewTimer(rl.regexDuration[idx])
		}
		matched := re.Match(r.Path)
		if measureRegex {
			timer.ObserveDuration()
		}
		if matched {
			return true
		}
	}
	return false
}
