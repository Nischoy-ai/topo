package mid

import (
	"strings"
	"testing"
)

func TestEncodeSOAPRequestUsesDocumentLiteralTableNamespace(t *testing.T) {
	encoded, err := encodeSOAPRequest("getRecords", []soapField{{Name: "__encoded_query", Value: "agent=mid.server.test^queue=output&state=ready"}})
	if err != nil {
		t.Fatal(err)
	}
	request := string(encoded)
	checks := []string{
		`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:ecc="http://www.service-now.com/ecc_queue">`,
		`<soapenv:Header/>`,
		`<ecc:getRecords>`,
		`<__encoded_query>agent=mid.server.test^queue=output&amp;state=ready</__encoded_query>`,
		`</ecc:getRecords>`,
	}
	for _, check := range checks {
		if !strings.Contains(request, check) {
			t.Fatalf("SOAP request does not contain %q: %s", check, request)
		}
	}
	if strings.Contains(request, `<ecc:__encoded_query>`) {
		t.Fatalf("document/literal child field was incorrectly namespace-qualified: %s", request)
	}
}

func TestValidateXMLBoundsRejectsDirectives(t *testing.T) {
	data := []byte(`<?xml version="1.0"?><!DOCTYPE Envelope><Envelope><Body/></Envelope>`)
	if err := validateXMLBounds(data); err == nil || !strings.Contains(err.Error(), "directives") {
		t.Fatalf("directive error = %v", err)
	}
}
