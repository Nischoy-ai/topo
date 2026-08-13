package lab

import (
	"encoding/json"
	"fmt"
	"io"
)

func DecodeScenario(r io.Reader) (Scenario, error) {
	var s Scenario
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Scenario{}, fmt.Errorf("decode scenario: %w", err)
	}
	if err := s.Validate(); err != nil {
		return Scenario{}, err
	}
	return s, nil
}
func EncodeScenario(w io.Writer, s Scenario) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}
