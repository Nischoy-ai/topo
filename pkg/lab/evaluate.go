package lab

import "github.com/Nischoy-ai/topo/pkg/model"

type Evaluation struct {
	ExpectedAssets   int     `json:"expected_assets"`
	ActualAssets     int     `json:"actual_assets"`
	MissingAssets    int     `json:"missing_assets"`
	UnexpectedAssets int     `json:"unexpected_assets"`
	CoveragePercent  float64 `json:"coverage_percent"`
}

func Evaluate(expected, actual model.ObservationEnvelope) Evaluation {
	want := map[string]struct{}{}
	got := map[string]struct{}{}
	for _, a := range expected.Assets {
		want[model.StableAssetID(a)] = struct{}{}
	}
	for _, a := range actual.Assets {
		got[model.StableAssetID(a)] = struct{}{}
	}
	missing := 0
	for id := range want {
		if _, ok := got[id]; !ok {
			missing++
		}
	}
	extra := 0
	for id := range got {
		if _, ok := want[id]; !ok {
			extra++
		}
	}
	coverage := 100.0
	if len(want) > 0 {
		coverage = 100 * float64(len(want)-missing) / float64(len(want))
	}
	return Evaluation{ExpectedAssets: len(want), ActualAssets: len(got), MissingAssets: missing, UnexpectedAssets: extra, CoveragePercent: coverage}
}
