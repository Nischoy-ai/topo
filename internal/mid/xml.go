package mid

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

const (
	soapEnvelopeNamespace = "http://schemas.xmlsoap.org/soap/envelope/"
	eccQueueNamespace     = "http://www.service-now.com/ecc_queue"
	maxSOAPResponseBytes  = 2 << 20
	maxXMLDepth           = 64
	maxXMLTokens          = 200000
)

type soapField struct {
	Name  string
	Value string
}

var soapNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func encodeSOAPRequest(operation string, fields []soapField) ([]byte, error) {
	if operation != "getRecords" && operation != "update" && operation != "insert" {
		return nil, fmt.Errorf("unsupported SOAP operation %q", operation)
	}
	var buffer bytes.Buffer
	buffer.WriteString(xml.Header)
	buffer.WriteString(`<soapenv:Envelope xmlns:soapenv="` + soapEnvelopeNamespace + `" xmlns:ecc="` + eccQueueNamespace + `"><soapenv:Header/><soapenv:Body><ecc:` + operation + `>`)
	for _, field := range fields {
		if !soapNamePattern.MatchString(field.Name) {
			return nil, errors.New("SOAP field name is invalid")
		}
		buffer.WriteByte('<')
		buffer.WriteString(field.Name)
		buffer.WriteByte('>')
		if err := xml.EscapeText(&buffer, []byte(field.Value)); err != nil {
			return nil, err
		}
		buffer.WriteString("</")
		buffer.WriteString(field.Name)
		buffer.WriteByte('>')
	}
	buffer.WriteString(`</ecc:` + operation + `></soapenv:Body></soapenv:Envelope>`)
	return buffer.Bytes(), nil
}

type soapEnvelope struct {
	Body soapBody `xml:"Body"`
}

type soapBody struct {
	Fault              *soapFault          `xml:"Fault"`
	GetRecordsResponse *getRecordsResponse `xml:"getRecordsResponse"`
	UpdateResponse     *mutationResponse   `xml:"updateResponse"`
	InsertResponse     *mutationResponse   `xml:"insertResponse"`
}

type soapFault struct {
	Code string `xml:"faultcode"`
}

type getRecordsResponse struct {
	Records []soapRecord `xml:"getRecordsResult"`
}

type mutationResponse struct {
	SysID string `xml:"sys_id"`
}

type soapRecord struct {
	SysID           string `xml:"sys_id"`
	Agent           string `xml:"agent"`
	Topic           string `xml:"topic"`
	Name            string `xml:"name"`
	Source          string `xml:"source"`
	Queue           string `xml:"queue"`
	State           string `xml:"state"`
	ResponseTo      string `xml:"response_to"`
	AgentCorrelator string `xml:"agent_correlator"`
	Parameters      string `xml:"parameters"`
	Payload         string `xml:"payload"`
	ErrorString     string `xml:"error_string"`
	CreatedOn       string `xml:"sys_created_on"`
}

func decodeSOAPEnvelope(data []byte) (soapEnvelope, error) {
	if len(data) == 0 {
		return soapEnvelope{}, errors.New("ServiceNow SOAP response is empty")
	}
	if len(data) > maxSOAPResponseBytes {
		return soapEnvelope{}, fmt.Errorf("ServiceNow SOAP response exceeds %d bytes", maxSOAPResponseBytes)
	}
	if err := validateXMLBounds(data); err != nil {
		return soapEnvelope{}, err
	}
	var envelope soapEnvelope
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	if err := decoder.Decode(&envelope); err != nil {
		return soapEnvelope{}, errors.New("ServiceNow SOAP response XML is invalid")
	}
	if envelope.Body.Fault != nil {
		// A hostile endpoint can place arbitrary request data in either fault
		// field. Do not reflect either field into an error that may be logged.
		return soapEnvelope{}, errors.New("ServiceNow SOAP fault")
	}
	return envelope, nil
}

func validateXMLBounds(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = true
	depth := 0
	tokens := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("ServiceNow SOAP XML is invalid")
		}
		tokens++
		if tokens > maxXMLTokens {
			return fmt.Errorf("ServiceNow SOAP XML exceeds %d tokens", maxXMLTokens)
		}
		switch token.(type) {
		case xml.StartElement:
			depth++
			if depth > maxXMLDepth {
				return fmt.Errorf("ServiceNow SOAP XML exceeds depth %d", maxXMLDepth)
			}
		case xml.EndElement:
			depth--
			if depth < 0 {
				return errors.New("ServiceNow SOAP XML has mismatched elements")
			}
		case xml.Directive:
			return errors.New("ServiceNow SOAP XML directives are not accepted")
		}
	}
	if depth != 0 {
		return errors.New("ServiceNow SOAP XML is truncated")
	}
	return nil
}

func (record soapRecord) model() Record {
	return Record{
		SysID:           strings.TrimSpace(record.SysID),
		Agent:           record.Agent,
		Topic:           record.Topic,
		Name:            record.Name,
		Source:          record.Source,
		Queue:           strings.ToLower(strings.TrimSpace(record.Queue)),
		State:           strings.ToLower(strings.TrimSpace(record.State)),
		ResponseTo:      strings.TrimSpace(record.ResponseTo),
		AgentCorrelator: record.AgentCorrelator,
		Parameters:      record.Parameters,
		Payload:         record.Payload,
		ErrorString:     record.ErrorString,
		CreatedOn:       record.CreatedOn,
	}
}
