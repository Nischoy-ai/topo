package winrm

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	ActionCreate        = namespaceTransfer + "/Create"
	ActionDelete        = namespaceTransfer + "/Delete"
	ActionCommand       = namespaceShell + "/Command"
	ActionReceive       = namespaceShell + "/Receive"
	ShellResourceURI    = namespaceShell + "/cmd"
	commandStateDone    = namespaceShell + "/CommandState/Done"
	commandStatePending = namespaceShell + "/CommandState/Pending"
	commandStateRunning = namespaceShell + "/CommandState/Running"
	maxRemoteIDBytes    = 512
	maxReceiveMessages  = 64
)

type receiveResult struct {
	Stdout, Stderr []byte
	Done           bool
	ExitCode       int
	HasExitCode    bool
}

func createShellEnvelope(endpoint string, timeout time.Duration) []byte {
	header := `<w:OptionSet><w:Option Name="WINRS_NOPROFILE">TRUE</w:Option><w:Option Name="WINRS_CODEPAGE">65001</w:Option></w:OptionSet>`
	body := `<r:Shell><r:InputStreams>stdin</r:InputStreams><r:OutputStreams>stdout stderr</r:OutputStreams></r:Shell>`
	return soapEnvelope(endpoint, ActionCreate, ShellResourceURI, timeout, header, body)
}

func commandEnvelope(endpoint, shellID string, command Command, timeout time.Duration) []byte {
	var body strings.Builder
	body.WriteString(`<r:CommandLine><r:Command>`)
	writeEscaped(&body, command.Executable)
	body.WriteString(`</r:Command>`)
	for _, argument := range command.Arguments {
		body.WriteString(`<r:Arguments>`)
		writeEscaped(&body, argument)
		body.WriteString(`</r:Arguments>`)
	}
	body.WriteString(`</r:CommandLine>`)
	header := selectorHeader(shellID) + `<w:OptionSet><w:Option Name="WINRS_CONSOLEMODE_STDIN">FALSE</w:Option><w:Option Name="WINRS_SKIP_CMD_SHELL">TRUE</w:Option></w:OptionSet>`
	return soapEnvelope(endpoint, ActionCommand, ShellResourceURI, timeout, header, body.String())
}

func receiveEnvelope(endpoint, shellID, commandID string, timeout time.Duration) []byte {
	var body strings.Builder
	body.WriteString(`<r:Receive><r:DesiredStream CommandId="`)
	writeEscaped(&body, commandID)
	body.WriteString(`">stdout stderr</r:DesiredStream></r:Receive>`)
	return soapEnvelope(endpoint, ActionReceive, ShellResourceURI, timeout, selectorHeader(shellID), body.String())
}

func deleteShellEnvelope(endpoint, shellID string, timeout time.Duration) []byte {
	return soapEnvelope(endpoint, ActionDelete, ShellResourceURI, timeout, selectorHeader(shellID), "")
}

func selectorHeader(shellID string) string {
	var header strings.Builder
	header.WriteString(`<w:SelectorSet><w:Selector Name="ShellId">`)
	writeEscaped(&header, shellID)
	header.WriteString(`</w:Selector></w:SelectorSet>`)
	return header.String()
}

func parseCreateShellResponse(data []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	validResponse := false
	var shellID string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if !validResponse {
				return "", errors.New("SOAP response is missing ResourceCreated")
			}
			if err := validateRemoteID("shell ID", shellID); err != nil {
				return "", err
			}
			return shellID, nil
		}
		if err != nil {
			return "", fmt.Errorf("decode shell creation response: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch {
		case start.Name.Space == namespaceTransfer && start.Name.Local == "ResourceCreated":
			validResponse = true
		case start.Name.Space == namespaceSOAP && start.Name.Local == "Fault":
			return "", parseFaultElement(decoder, start)
		case start.Name.Space == namespaceWSMan && start.Name.Local == "Selector" && strings.EqualFold(attribute(start, "Name"), "ShellId"):
			var value string
			if err := decoder.DecodeElement(&value, &start); err != nil {
				return "", err
			}
			value = strings.TrimSpace(value)
			if shellID != "" && shellID != value {
				return "", errors.New("shell creation response contains conflicting shell IDs")
			}
			shellID = value
		}
	}
}

func parseCommandResponse(data []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	validResponse := false
	var commandID string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if !validResponse {
				return "", errors.New("SOAP response is missing CommandResponse")
			}
			if err := validateRemoteID("command ID", commandID); err != nil {
				return "", err
			}
			return commandID, nil
		}
		if err != nil {
			return "", fmt.Errorf("decode command response: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch {
		case start.Name.Space == namespaceShell && start.Name.Local == "CommandResponse":
			validResponse = true
		case start.Name.Space == namespaceSOAP && start.Name.Local == "Fault":
			return "", parseFaultElement(decoder, start)
		case start.Name.Space == namespaceShell && start.Name.Local == "CommandId":
			if commandID != "" {
				return "", errors.New("command response contains duplicate command ID")
			}
			if err := decoder.DecodeElement(&commandID, &start); err != nil {
				return "", err
			}
			commandID = strings.TrimSpace(commandID)
		}
	}
}

func parseReceiveResponse(data []byte, expectedCommandID string) (receiveResult, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return receiveResult{}, errors.New("SOAP response is missing ReceiveResponse")
		}
		if err != nil {
			return receiveResult{}, fmt.Errorf("decode receive response: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Space == namespaceSOAP && start.Name.Local == "Fault" {
			return receiveResult{}, parseFaultElement(decoder, start)
		}
		if start.Name.Space != namespaceShell || start.Name.Local != "ReceiveResponse" {
			continue
		}
		return decodeReceiveResponse(decoder, start, expectedCommandID)
	}
}

func decodeReceiveResponse(decoder *xml.Decoder, start xml.StartElement, expectedCommandID string) (receiveResult, error) {
	var response struct {
		Streams []struct {
			Name      string `xml:"Name,attr"`
			CommandID string `xml:"CommandId,attr"`
			Data      string `xml:",chardata"`
		} `xml:"Stream"`
		State struct {
			Value     string `xml:"State,attr"`
			CommandID string `xml:"CommandId,attr"`
			ExitCode  string `xml:"ExitCode"`
		} `xml:"CommandState"`
	}
	if err := decoder.DecodeElement(&response, &start); err != nil {
		return receiveResult{}, fmt.Errorf("decode ReceiveResponse: %w", err)
	}
	var result receiveResult
	for _, stream := range response.Streams {
		if stream.CommandID != "" && stream.CommandID != expectedCommandID {
			return receiveResult{}, errors.New("receive response contains an unexpected command ID")
		}
		encoded := strings.Join(strings.Fields(stream.Data), "")
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return receiveResult{}, fmt.Errorf("decode %s stream: %w", stream.Name, err)
		}
		switch stream.Name {
		case "stdout":
			result.Stdout = append(result.Stdout, decoded...)
		case "stderr":
			result.Stderr = append(result.Stderr, decoded...)
		default:
			return receiveResult{}, fmt.Errorf("receive response contains unsupported stream %q", stream.Name)
		}
	}
	if response.State.CommandID != "" && response.State.CommandID != expectedCommandID {
		return receiveResult{}, errors.New("command state contains an unexpected command ID")
	}
	if response.State.Value != "" && response.State.Value != commandStateDone && response.State.Value != commandStatePending && response.State.Value != commandStateRunning {
		return receiveResult{}, fmt.Errorf("command state contains unsupported value %q", response.State.Value)
	}
	result.Done = response.State.Value == commandStateDone
	if strings.TrimSpace(response.State.ExitCode) != "" {
		exitCode, err := strconv.Atoi(strings.TrimSpace(response.State.ExitCode))
		if err != nil || exitCode < 0 {
			return receiveResult{}, fmt.Errorf("invalid remote exit code %q", response.State.ExitCode)
		}
		result.ExitCode = exitCode
		result.HasExitCode = true
	}
	if result.Done && !result.HasExitCode {
		return receiveResult{}, errors.New("completed command response omitted exit code")
	}
	return result, nil
}

func parseEmptySOAPResponse(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	seenEnvelope := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			if !seenEnvelope {
				return errors.New("empty SOAP response")
			}
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode SOAP response: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		if start.Name.Space == namespaceSOAP && start.Name.Local == "Envelope" {
			seenEnvelope = true
		}
		if start.Name.Space == namespaceSOAP && start.Name.Local == "Fault" {
			return parseFaultElement(decoder, start)
		}
	}
}

func parseFaultElement(decoder *xml.Decoder, start xml.StartElement) error {
	texts, err := elementTexts(decoder, start)
	if err != nil {
		return err
	}
	return fmt.Errorf("WS-Management fault: %s", boundedFaultText(texts))
}

func parseSOAPFault(data []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return errors.New("response did not contain a SOAP fault")
		}
		if err != nil {
			return fmt.Errorf("decode SOAP fault: %w", err)
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Space == namespaceSOAP && start.Name.Local == "Fault" {
			return parseFaultElement(decoder, start)
		}
	}
}

func validateRemoteID(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is empty", name)
	}
	if len(value) > maxRemoteIDBytes {
		return fmt.Errorf("%s exceeds %d bytes", name, maxRemoteIDBytes)
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return fmt.Errorf("%s contains a control character", name)
	}
	return nil
}
