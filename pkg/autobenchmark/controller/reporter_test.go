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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	abtypes "sigs.k8s.io/rbgs/pkg/autobenchmark/types"
)

func TestSelectBest(t *testing.T) {
	tests := []struct {
		name   string
		trials []abtypes.TrialResult
		want   *abtypes.TrialResult
	}{
		{
			name:   "empty",
			trials: nil,
			want:   nil,
		},
		{
			name: "all fail SLA",
			trials: []abtypes.TrialResult{
				{TrialIndex: 0, SLAPass: false, Score: 0},
				{TrialIndex: 1, SLAPass: false, Score: 0},
			},
			want: nil,
		},
		{
			name: "single pass",
			trials: []abtypes.TrialResult{
				{TrialIndex: 0, SLAPass: false, Score: 0},
				{TrialIndex: 1, SLAPass: true, Score: 1500},
			},
			want: &abtypes.TrialResult{TrialIndex: 1, SLAPass: true, Score: 1500},
		},
		{
			name: "multiple pass - highest score wins",
			trials: []abtypes.TrialResult{
				{TrialIndex: 0, SLAPass: true, Score: 1000},
				{TrialIndex: 1, SLAPass: true, Score: 2000},
				{TrialIndex: 2, SLAPass: true, Score: 1500},
				{TrialIndex: 3, SLAPass: false, Score: 0},
			},
			want: &abtypes.TrialResult{TrialIndex: 1, SLAPass: true, Score: 2000},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			best := SelectBest(tt.trials)
			if tt.want == nil {
				assert.Nil(t, best)
			} else {
				require.NotNil(t, best)
				assert.Equal(t, tt.want.TrialIndex, best.TrialIndex)
				assert.InDelta(t, tt.want.Score, best.Score, 0.01)
			}
		})
	}
}

func TestBuildReport(t *testing.T) {
	t.Run("with global best", func(t *testing.T) {
		state := &abtypes.ExperimentState{
			ExperimentID: "exp-1",
			Templates: []abtypes.TemplateState{
				{
					Name: "t1",
					Trials: []abtypes.TrialResult{
						{TrialIndex: 0, SLAPass: true, Score: 1000},
						{TrialIndex: 1, SLAPass: false},
					},
					BestTrial: &abtypes.TrialResult{TrialIndex: 0, Score: 1000},
				},
				{
					Name: "t2",
					Trials: []abtypes.TrialResult{
						{TrialIndex: 0, SLAPass: true, Score: 2000},
					},
					BestTrial: &abtypes.TrialResult{TrialIndex: 0, Score: 2000},
				},
			},
			GlobalBest: &abtypes.TrialResult{TrialIndex: 0, TemplateName: "t2", Score: 2000},
		}

		report := BuildReport(state)
		assert.Equal(t, "exp-1", report.ExperimentID)
		require.NotNil(t, report.GlobalBest)
		assert.InDelta(t, 2000, report.GlobalBest.Score, 0.01)
		require.Len(t, report.Templates, 2)
		assert.Equal(t, 2, report.Templates[0].NumTrials)
		assert.Equal(t, 1, report.Templates[0].NumSLAPass)
		assert.Equal(t, 1, report.Templates[1].NumTrials)
		assert.Equal(t, 1, report.Templates[1].NumSLAPass)
		assert.Contains(t, report.Summary, "Best result")
		assert.Contains(t, report.Summary, "2/3 trials passed SLA")
	})

	t.Run("no SLA pass", func(t *testing.T) {
		state := &abtypes.ExperimentState{
			ExperimentID: "exp-2",
			Templates: []abtypes.TemplateState{
				{
					Name: "t1",
					Trials: []abtypes.TrialResult{
						{TrialIndex: 0, SLAPass: false},
						{TrialIndex: 1, SLAPass: false},
					},
				},
			},
		}

		report := BuildReport(state)
		assert.Nil(t, report.GlobalBest)
		assert.Contains(t, report.Summary, "No configuration met SLA")
		assert.Contains(t, report.Summary, "2 trials executed")
	})
}

func TestWriteReportJSON(t *testing.T) {
	dir := t.TempDir()
	report := &Report{
		ExperimentID: "exp-test",
		Summary:      "test summary",
	}

	err := WriteReportJSON(dir, report)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "report.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "exp-test")
	assert.Contains(t, string(data), "test summary")
}

func TestWriteReportYAML(t *testing.T) {
	dir := t.TempDir()
	report := &Report{
		ExperimentID: "exp-test",
		Summary:      "test summary",
	}

	err := WriteReportYAML(dir, report)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "report.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "exp-test")
	assert.Contains(t, string(data), "test summary")
}
