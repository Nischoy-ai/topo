// Package mid implements the bounded native ServiceNow ECC/SOAP transport
// used by Topo's ECC-compatible MID mode. Operation-specific discovery
// contracts are intentionally separate from this transport boundary.
package mid

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	AgentPrefix       = "mid.server."
	TopicHeartbeat    = "Heartbeat"
	QueueInput        = "input"
	QueueOutput       = "output"
	StateReady        = "ready"
	StateProcessing   = "processing"
	StateProcessed    = "processed"
	UnsupportedCode   = "topo_unsupported_topic"
	ClaimConflictCode = "topo_claim_conflict"

	maxMIDNameBytes       = 128
	maxShortFieldBytes    = 512
	maxParametersBytes    = 256 << 10
	maxPayloadBytes       = 1 << 20
	maxErrorStringBytes   = 4096
	maxECCRecordJSONBytes = 2 << 20
)

var (
	midNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	sysIDPattern   = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

// Record is the bounded subset of ecc_queue used by this slice. Parameters
// and payload remain opaque data; dispatchers never interpret them as code.
type Record struct {
	SysID           string `json:"sys_id,omitempty"`
	Agent           string `json:"agent"`
	Topic           string `json:"topic"`
	Name            string `json:"name,omitempty"`
	Source          string `json:"source,omitempty"`
	Queue           string `json:"queue"`
	State           string `json:"state"`
	ResponseTo      string `json:"response_to,omitempty"`
	AgentCorrelator string `json:"agent_correlator,omitempty"`
	Parameters      string `json:"parameters,omitempty"`
	Payload         string `json:"payload,omitempty"`
	ErrorString     string `json:"error_string,omitempty"`
	CreatedOn       string `json:"sys_created_on,omitempty"`
}

func AgentName(midName string) (string, error) {
	if !midNamePattern.MatchString(midName) {
		return "", fmt.Errorf("MID name must use 1-%d letters, digits, dots, underscores, or hyphens and start with a letter or digit", maxMIDNameBytes)
	}
	return AgentPrefix + midName, nil
}

func validateOutputRecord(record Record, agent string) error {
	if !sysIDPattern.MatchString(record.SysID) {
		return errors.New("ServiceNow ECC record has an invalid sys_id")
	}
	if record.Agent != agent {
		return errors.New("ServiceNow ECC record is addressed to a different agent")
	}
	if record.Queue != QueueOutput {
		return errors.New("ServiceNow ECC record is not in the output queue")
	}
	if record.State != StateReady && record.State != StateProcessing && record.State != StateProcessed {
		return errors.New("ServiceNow ECC record has an unsupported state")
	}
	return validateRecordFields(record)
}

func validateInputRecord(record Record, agent string) error {
	if record.SysID != "" && !sysIDPattern.MatchString(record.SysID) {
		return errors.New("ServiceNow ECC response has an invalid sys_id")
	}
	if record.Agent != agent || record.Queue != QueueInput || record.State != StateReady {
		return errors.New("ServiceNow ECC response has invalid agent, queue, or state")
	}
	if !sysIDPattern.MatchString(record.ResponseTo) {
		return errors.New("ServiceNow ECC response has an invalid response_to")
	}
	return validateRecordFields(record)
}

func validateRecordFields(record Record) error {
	for name, value := range map[string]string{
		"agent":            record.Agent,
		"topic":            record.Topic,
		"name":             record.Name,
		"source":           record.Source,
		"queue":            record.Queue,
		"state":            record.State,
		"response_to":      record.ResponseTo,
		"agent_correlator": record.AgentCorrelator,
		"sys_created_on":   record.CreatedOn,
	} {
		if len(value) > maxShortFieldBytes {
			return fmt.Errorf("ServiceNow ECC %s exceeds %d bytes", name, maxShortFieldBytes)
		}
		if hasControl(value) {
			return fmt.Errorf("ServiceNow ECC %s contains control characters", name)
		}
	}
	if record.Topic == "" {
		return errors.New("ServiceNow ECC topic is empty")
	}
	if len(record.Parameters) > maxParametersBytes {
		return fmt.Errorf("ServiceNow ECC parameters exceed %d bytes", maxParametersBytes)
	}
	if len(record.Payload) > maxPayloadBytes {
		return fmt.Errorf("ServiceNow ECC payload exceeds %d bytes", maxPayloadBytes)
	}
	if len(record.ErrorString) > maxErrorStringBytes {
		return fmt.Errorf("ServiceNow ECC error_string exceeds %d bytes", maxErrorStringBytes)
	}
	return nil
}

func recordDigest(record Record) (string, error) {
	// State is deliberately excluded: claim changes ready to processing while
	// every operation-bearing field must remain identical.
	record.State = ""
	record.CreatedOn = ""
	encoded, err := json.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("encode ECC record digest: %w", err)
	}
	if len(encoded) > maxECCRecordJSONBytes {
		return "", errors.New("ECC record exceeds the local journal bound")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func hasControl(value string) bool {
	return strings.IndexFunc(value, func(r rune) bool {
		return r < 0x20 || r == 0x7f
	}) >= 0
}

func boundedText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
