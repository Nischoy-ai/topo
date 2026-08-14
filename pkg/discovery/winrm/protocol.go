package winrm

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	namespaceSOAP     = "http://www.w3.org/2003/05/soap-envelope"
	namespaceAddress  = "http://schemas.xmlsoap.org/ws/2004/08/addressing"
	namespaceWSMan    = "http://schemas.dmtf.org/wbem/wsman/1/wsman.xsd"
	namespaceEnum     = "http://schemas.xmlsoap.org/ws/2004/09/enumeration"
	anonymousReplyURI = namespaceAddress + "/role/anonymous"
)

// SOAPRequest is the security-relevant portion of an incoming WS-Management
// request. Topo Lab parses this before routing an audited operation.
type SOAPRequest struct {
	Action             string
	ResourceURI        string
	Query              string
	FilterDialect      string
	EnumerationContext string
	BodyOperation      string
}

// ParseSOAPRequest extracts the fixed routing fields without interpreting
// arbitrary commands or scripts.
func ParseSOAPRequest(data []byte) (SOAPRequest, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var request SOAPRequest
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return SOAPRequest{}, fmt.Errorf("decode SOAP request: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		var destination *string
		switch start.Name.Local {
		case "Enumerate", "Pull":
			if request.BodyOperation != "" {
				return SOAPRequest{}, errors.New("duplicate SOAP body operation")
			}
			if start.Name.Space != namespaceEnum {
				return SOAPRequest{}, fmt.Errorf("invalid namespace for SOAP body operation %s", start.Name.Local)
			}
			request.BodyOperation = start.Name.Local
		case "Command", "Create", "Invoke":
			if request.BodyOperation != "" {
				return SOAPRequest{}, errors.New("duplicate SOAP body operation")
			}
			request.BodyOperation = start.Name.Local
		case "Action":
			if start.Name.Space != namespaceAddress {
				return SOAPRequest{}, errors.New("invalid namespace for SOAP action")
			}
			destination = &request.Action
		case "ResourceURI":
			if start.Name.Space != namespaceWSMan {
				return SOAPRequest{}, errors.New("invalid namespace for SOAP resource URI")
			}
			destination = &request.ResourceURI
		case "Filter":
			if start.Name.Space != namespaceWSMan {
				return SOAPRequest{}, errors.New("invalid namespace for SOAP filter")
			}
			destination = &request.Query
			for _, attribute := range start.Attr {
				if attribute.Name.Local == "Dialect" {
					request.FilterDialect = strings.TrimSpace(attribute.Value)
				}
			}
		case "EnumerationContext":
			if start.Name.Space != namespaceEnum {
				return SOAPRequest{}, errors.New("invalid namespace for enumeration context")
			}
			destination = &request.EnumerationContext
		}
		if destination != nil {
			if *destination != "" {
				return SOAPRequest{}, fmt.Errorf("duplicate SOAP field %s", start.Name.Local)
			}
			if err := decoder.DecodeElement(destination, &start); err != nil {
				return SOAPRequest{}, fmt.Errorf("decode SOAP %s: %w", start.Name.Local, err)
			}
			*destination = strings.TrimSpace(*destination)
		}
	}
	if request.Action == "" || request.ResourceURI == "" || request.BodyOperation == "" {
		return SOAPRequest{}, errors.New("SOAP action, resource URI, and body operation are required")
	}
	return request, nil
}

type enumerationPage struct {
	Context string
	Done    bool
	Items   []object
}

type object map[string][]string

func enumerateEnvelope(endpoint string, operation Operation, timeout time.Duration) []byte {
	var body strings.Builder
	body.WriteString(`<n:Enumerate>`)
	body.WriteString(`<w:OptimizeEnumeration/>`)
	body.WriteString(`<w:MaxElements>128</w:MaxElements>`)
	if operation.Query != "" {
		body.WriteString(`<w:Filter Dialect="`)
		body.WriteString(DialectWQL)
		body.WriteString(`">`)
		writeEscaped(&body, operation.Query)
		body.WriteString(`</w:Filter>`)
	}
	body.WriteString(`</n:Enumerate>`)
	return soapEnvelope(endpoint, ActionEnumerate, operation.ResourceURI, timeout, body.String())
}

func pullEnvelope(endpoint string, operation Operation, context string, timeout time.Duration) []byte {
	var body strings.Builder
	body.WriteString(`<n:Pull><n:EnumerationContext>`)
	writeEscaped(&body, context)
	body.WriteString(`</n:EnumerationContext><n:MaxElements>128</n:MaxElements></n:Pull>`)
	return soapEnvelope(endpoint, ActionPull, operation.ResourceURI, timeout, body.String())
}

func soapEnvelope(endpoint, action, resourceURI string, timeout time.Duration, body string) []byte {
	seconds := int64((timeout + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	var envelope strings.Builder
	envelope.WriteString(`<?xml version="1.0" encoding="UTF-8"?>`)
	envelope.WriteString(`<s:Envelope xmlns:s="` + namespaceSOAP + `" xmlns:a="` + namespaceAddress + `" xmlns:w="` + namespaceWSMan + `" xmlns:n="` + namespaceEnum + `"><s:Header>`)
	envelope.WriteString(`<a:To s:mustUnderstand="true">`)
	writeEscaped(&envelope, endpoint)
	envelope.WriteString(`</a:To><w:ResourceURI s:mustUnderstand="true">`)
	writeEscaped(&envelope, resourceURI)
	envelope.WriteString(`</w:ResourceURI><a:ReplyTo><a:Address>` + anonymousReplyURI + `</a:Address></a:ReplyTo><a:Action s:mustUnderstand="true">`)
	writeEscaped(&envelope, action)
	envelope.WriteString(`</a:Action><w:MaxEnvelopeSize s:mustUnderstand="true">4194304</w:MaxEnvelopeSize><a:MessageID>uuid:` + messageID() + `</a:MessageID><w:OperationTimeout>PT`)
	envelope.WriteString(fmt.Sprintf("%dS", seconds))
	envelope.WriteString(`</w:OperationTimeout></s:Header><s:Body>`)
	envelope.WriteString(body)
	envelope.WriteString(`</s:Body></s:Envelope>`)
	return []byte(envelope.String())
}

func parseEnumerationPage(data []byte) (enumerationPage, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	var page enumerationPage
	validResponse := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if !validResponse {
				return enumerationPage{}, errors.New("SOAP response is missing EnumerateResponse or PullResponse")
			}
			return page, nil
		}
		if err != nil {
			return enumerationPage{}, fmt.Errorf("decode SOAP response: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "EnumerateResponse", "PullResponse":
			validResponse = true
		case "Fault":
			texts, err := elementTexts(decoder, start)
			if err != nil {
				return enumerationPage{}, err
			}
			return enumerationPage{}, fmt.Errorf("WS-Management fault: %s", boundedFaultText(texts))
		case "EnumerationContext":
			if err := decoder.DecodeElement(&page.Context, &start); err != nil {
				return enumerationPage{}, err
			}
			page.Context = strings.TrimSpace(page.Context)
		case "EndOfSequence":
			page.Done = true
			if err := decoder.Skip(); err != nil {
				return enumerationPage{}, err
			}
		case "Items":
			items, err := parseItems(decoder, start)
			if err != nil {
				return enumerationPage{}, err
			}
			page.Items = append(page.Items, items...)
		}
	}
}

func boundedFaultText(texts []string) string {
	value := strings.Join(strings.Fields(strings.Join(texts, " ")), " ")
	const maximum = 512
	if len(value) > maximum {
		return value[:maximum] + "..."
	}
	return value
}

func parseItems(decoder *xml.Decoder, itemsStart xml.StartElement) ([]object, error) {
	var items []object
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			item, err := parseObject(decoder, value)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		case xml.EndElement:
			if value.Name == itemsStart.Name {
				return items, nil
			}
		}
	}
}

func parseObject(decoder *xml.Decoder, objectStart xml.StartElement) (object, error) {
	item := object{}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.StartElement:
			texts, err := elementTexts(decoder, value)
			if err != nil {
				return nil, err
			}
			item[value.Name.Local] = append(item[value.Name.Local], texts...)
		case xml.EndElement:
			if value.Name == objectStart.Name {
				return item, nil
			}
		}
	}
}

func elementTexts(decoder *xml.Decoder, start xml.StartElement) ([]string, error) {
	var texts []string
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch value := token.(type) {
		case xml.CharData:
			if text := strings.TrimSpace(string(value)); text != "" {
				texts = append(texts, text)
			}
		case xml.StartElement:
			children, err := elementTexts(decoder, value)
			if err != nil {
				return nil, err
			}
			texts = append(texts, children...)
		case xml.EndElement:
			if value.Name == start.Name {
				return texts, nil
			}
		}
	}
}

func writeEscaped(builder *strings.Builder, value string) {
	_ = xml.EscapeText(builder, []byte(value))
}

func messageID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]
}
