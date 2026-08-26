package mid

import (
	"encoding/xml"
	"fmt"
)

type resultPayload struct {
	XMLName    xml.Name          `xml:"results"`
	Result     resultPayloadItem `xml:"result"`
	Parameters struct{}          `xml:"parameters"`
}

type resultPayloadItem struct {
	Status  string `xml:"status,attr"`
	Code    string `xml:"code,attr,omitempty"`
	Message string `xml:"message,attr,omitempty"`
	Version string `xml:"version,attr,omitempty"`
}

// Dispatch recognizes only the stock Heartbeat topic in the first native
// slice. All other topics get a visible correlated denial and no payload,
// name, source, or parameters are interpreted as executable content.
func Dispatch(record Record, version string) (Record, error) {
	result := Record{
		Agent:           record.Agent,
		Topic:           record.Topic,
		Name:            record.Name,
		Source:          record.Source,
		Queue:           QueueInput,
		State:           StateReady,
		ResponseTo:      record.SysID,
		AgentCorrelator: record.AgentCorrelator,
		Parameters:      record.Parameters,
	}
	payload := resultPayload{}
	switch record.Topic {
	case TopicHeartbeat:
		payload.Result = resultPayloadItem{
			Status:  "success",
			Message: "Topo ECC heartbeat acknowledged",
			Version: boundedText(version, 128),
		}
	default:
		message := "Topo denied an unsupported ECC topic"
		payload.Result = resultPayloadItem{Status: "error", Code: UnsupportedCode, Message: message}
		result.Name = "Topo unsupported ECC topic"
		result.ErrorString = message
	}
	encoded, err := xml.Marshal(payload)
	if err != nil {
		return Record{}, fmt.Errorf("encode ECC result payload: %w", err)
	}
	result.Payload = string(encoded)
	if err := validateInputRecord(result, record.Agent); err != nil {
		return Record{}, err
	}
	return result, nil
}

func dispatchError(record Record, code, message string) (Record, error) {
	result := Record{
		Agent:           record.Agent,
		Topic:           record.Topic,
		Name:            "Topo denied ECC record",
		Source:          record.Source,
		Queue:           QueueInput,
		State:           StateReady,
		ResponseTo:      record.SysID,
		AgentCorrelator: record.AgentCorrelator,
		Parameters:      record.Parameters,
		ErrorString:     boundedText(message, maxErrorStringBytes),
	}
	payload := resultPayload{Result: resultPayloadItem{
		Status:  "error",
		Code:    boundedText(code, 128),
		Message: result.ErrorString,
	}}
	encoded, err := xml.Marshal(payload)
	if err != nil {
		return Record{}, fmt.Errorf("encode ECC error payload: %w", err)
	}
	result.Payload = string(encoded)
	if err := validateInputRecord(result, record.Agent); err != nil {
		return Record{}, err
	}
	return result, nil
}
