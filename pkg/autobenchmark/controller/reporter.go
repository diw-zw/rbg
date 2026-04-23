/*
Copyright 2026 The RBG Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v2"

	abtypes "sigs.k8s.io/rbgs/pkg/autobenchmark/types"
)

// Report is the final output of an auto-benchmark experiment.
type Report struct {
	ExperimentID string               `json:"experimentId" yaml:"experimentId"`
	GlobalBest   *abtypes.TrialResult `json:"globalBest,omitempty" yaml:"globalBest,omitempty"`
	Templates    []TemplateReport     `json:"templates" yaml:"templates"`
	Summary      string               `json:"summary" yaml:"summary"`
}

// TemplateReport summarizes results for a single template.
type TemplateReport struct {
	Name       string               `json:"name" yaml:"name"`
	BestTrial  *abtypes.TrialResult `json:"bestTrial,omitempty" yaml:"bestTrial,omitempty"`
	NumTrials  int                  `json:"numTrials" yaml:"numTrials"`
	NumSLAPass int                  `json:"numSLAPass" yaml:"numSLAPass"`
}

// BuildReport creates a report from the experiment state.
func BuildReport(state *abtypes.ExperimentState) *Report {
	report := &Report{
		ExperimentID: state.ExperimentID,
		GlobalBest:   state.GlobalBest,
	}

	totalTrials := 0
	totalPass := 0

	for _, ts := range state.Templates {
		tr := TemplateReport{
			Name:      ts.Name,
			BestTrial: ts.BestTrial,
			NumTrials: len(ts.Trials),
		}
		for _, trial := range ts.Trials {
			if trial.SLAPass {
				tr.NumSLAPass++
			}
		}
		totalTrials += tr.NumTrials
		totalPass += tr.NumSLAPass
		report.Templates = append(report.Templates, tr)
	}

	if state.GlobalBest != nil {
		report.Summary = fmt.Sprintf(
			"Best result: template=%q, trial=%d, score=%.2f (%d/%d trials passed SLA)",
			state.GlobalBest.TemplateName, state.GlobalBest.TrialIndex,
			state.GlobalBest.Score, totalPass, totalTrials,
		)
	} else {
		report.Summary = fmt.Sprintf(
			"No configuration met SLA constraints (%d trials executed across %d templates)",
			totalTrials, len(state.Templates),
		)
	}

	return report
}

// SelectBest finds the best SLA-passing trial from a list by highest score.
func SelectBest(trials []abtypes.TrialResult) *abtypes.TrialResult {
	var best *abtypes.TrialResult
	for i := range trials {
		t := &trials[i]
		if !t.SLAPass {
			continue
		}
		if best == nil || t.Score > best.Score {
			best = t
		}
	}
	return best
}

// WriteReportJSON writes the report as JSON to the given directory.
func WriteReportJSON(dir string, report *Report) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "report.json"), data, 0644)
}

// WriteReportYAML writes the report as YAML to the given directory.
func WriteReportYAML(dir string, report *Report) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(report)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "report.yaml"), data, 0644)
}
