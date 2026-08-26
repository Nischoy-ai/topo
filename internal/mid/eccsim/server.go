// Package eccsim provides a deterministic in-memory simulation of the
// documented ServiceNow direct SOAP operations used by Topo's ECC transport.
// It validates transport behavior only; it does not simulate stock sensors,
// MID registration, validation, or instance liveness decisions.
package eccsim

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Nischoy-ai/topo/internal/mid"
)

const (
	maxRequestBytes = 2 << 20
	soapActionBase  = "http://www.service-now.com/ecc_queue/"
)

type Server struct {
	username string
	password string

	mu         sync.Mutex
	records    map[string]mid.Record
	nextID     uint64
	operations []string
}

func New(username, password string) *Server {
	return &Server{
		username: username,
		password: password,
		records:  make(map[string]mid.Record),
		nextID:   1,
	}
}

func (s *Server) AddOutput(agent, topic, name, source, parameters, payload, correlator string) mid.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := mid.Record{
		SysID:           s.nextSysIDLocked(),
		Agent:           agent,
		Topic:           topic,
		Name:            name,
		Source:          source,
		Queue:           mid.QueueOutput,
		State:           mid.StateReady,
		AgentCorrelator: correlator,
		Parameters:      parameters,
		Payload:         payload,
		CreatedOn:       time.Unix(int64(s.nextID), 0).UTC().Format("2006-01-02 15:04:05"),
	}
	s.records[record.SysID] = record
	return record
}

func (s *Server) Records() []mid.Record {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recordsLocked()
}

func (s *Server) Operations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.operations...)
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.URL.Path != "/ecc_queue.do" || request.URL.RawQuery != "SOAP" {
		http.NotFound(writer, request)
		return
	}
	username, password, ok := request.BasicAuth()
	if !ok || username != s.username || password != s.password {
		writer.Header().Set("WWW-Authenticate", `Basic realm="ServiceNow"`)
		writeFault(writer, http.StatusUnauthorized, "Client.Auth", "authentication failed")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		writeFault(writer, http.StatusBadRequest, "Client.Request", "request exceeds bound")
		return
	}
	var envelope requestEnvelope
	if err := xml.Unmarshal(body, &envelope); err != nil {
		writeFault(writer, http.StatusBadRequest, "Client.XML", "invalid SOAP XML")
		return
	}

	operation := ""
	switch {
	case envelope.Body.GetRecords != nil:
		operation = "getRecords"
	case envelope.Body.Update != nil:
		operation = "update"
	case envelope.Body.Insert != nil:
		operation = "insert"
	default:
		writeFault(writer, http.StatusBadRequest, "Client.Operation", "unsupported SOAP operation")
		return
	}
	if request.Header.Get("SOAPAction") != soapActionBase+operation {
		writeFault(writer, http.StatusBadRequest, "Client.SOAPAction", "invalid SOAPAction")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch operation {
	case "getRecords":
		s.operations = append(s.operations, "getRecords")
		s.getRecords(writer, *envelope.Body.GetRecords)
	case "update":
		s.operations = append(s.operations, "update")
		s.update(writer, *envelope.Body.Update)
	case "insert":
		s.operations = append(s.operations, "insert")
		s.insert(writer, *envelope.Body.Insert)
	}
}

type requestEnvelope struct {
	Body requestBody `xml:"Body"`
}

type requestBody struct {
	GetRecords *getRecordsRequest `xml:"getRecords"`
	Update     *updateRequest     `xml:"update"`
	Insert     *insertRequest     `xml:"insert"`
}

type getRecordsRequest struct {
	EncodedQuery string `xml:"__encoded_query"`
	FirstRow     string `xml:"__first_row"`
	LastRow      string `xml:"__last_row"`
}

type updateRequest struct {
	SysID string `xml:"sys_id"`
	State string `xml:"state"`
}

type insertRequest wireRecord

func (s *Server) getRecords(writer http.ResponseWriter, request getRecordsRequest) {
	first, err := strconv.Atoi(request.FirstRow)
	if err != nil || first < 0 {
		writeFault(writer, http.StatusBadRequest, "Client.Query", "invalid first row")
		return
	}
	last, err := strconv.Atoi(request.LastRow)
	if err != nil || last < first || last-first > 16 {
		writeFault(writer, http.StatusBadRequest, "Client.Query", "invalid last row")
		return
	}
	filters := make(map[string]string)
	for _, term := range strings.Split(request.EncodedQuery, "^") {
		if term == "" || strings.HasPrefix(term, "ORDERBY") {
			continue
		}
		name, value, ok := strings.Cut(term, "=")
		if !ok {
			writeFault(writer, http.StatusBadRequest, "Client.Query", "invalid encoded query")
			return
		}
		switch name {
		case "sys_id", "agent", "queue", "state", "response_to":
			filters[name] = value
		default:
			writeFault(writer, http.StatusBadRequest, "Client.Query", "unsupported encoded query field")
			return
		}
	}
	records := s.recordsLocked()
	filtered := records[:0]
	for _, record := range records {
		if matches(record, filters) {
			filtered = append(filtered, record)
		}
	}
	if first > len(filtered) {
		first = len(filtered)
	}
	if last > len(filtered) {
		last = len(filtered)
	}
	response := getRecordsEnvelope{}
	for _, record := range filtered[first:last] {
		response.Body.Response.Records = append(response.Body.Response.Records, fromRecord(record))
	}
	writeXML(writer, http.StatusOK, response)
}

func (s *Server) update(writer http.ResponseWriter, request updateRequest) {
	record, ok := s.records[request.SysID]
	if !ok {
		writeFault(writer, http.StatusNotFound, "Client.Record", "record not found")
		return
	}
	if request.State != mid.StateProcessing && request.State != mid.StateProcessed {
		writeFault(writer, http.StatusBadRequest, "Client.State", "unsupported state transition")
		return
	}
	record.State = request.State
	s.records[request.SysID] = record
	response := mutationEnvelope{}
	response.Body.Update = &mutationResponse{SysID: request.SysID}
	writeXML(writer, http.StatusOK, response)
}

func (s *Server) insert(writer http.ResponseWriter, request insertRequest) {
	record := wireRecord(request).model()
	record.SysID = s.nextSysIDLocked()
	record.CreatedOn = time.Unix(int64(s.nextID), 0).UTC().Format("2006-01-02 15:04:05")
	s.records[record.SysID] = record
	response := mutationEnvelope{}
	response.Body.Insert = &mutationResponse{SysID: record.SysID}
	writeXML(writer, http.StatusOK, response)
}

func (s *Server) nextSysIDLocked() string {
	value := fmt.Sprintf("%032x", s.nextID)
	s.nextID++
	return value
}

func (s *Server) recordsLocked() []mid.Record {
	records := make([]mid.Record, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedOn != records[j].CreatedOn {
			return records[i].CreatedOn < records[j].CreatedOn
		}
		return records[i].SysID < records[j].SysID
	})
	return records
}

func matches(record mid.Record, filters map[string]string) bool {
	for field, value := range filters {
		switch field {
		case "sys_id":
			if record.SysID != value {
				return false
			}
		case "agent":
			if record.Agent != value {
				return false
			}
		case "queue":
			if record.Queue != value {
				return false
			}
		case "state":
			if record.State != value {
				return false
			}
		case "response_to":
			if record.ResponseTo != value {
				return false
			}
		}
	}
	return true
}

type wireRecord struct {
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

func fromRecord(record mid.Record) wireRecord {
	return wireRecord{
		SysID:           record.SysID,
		Agent:           record.Agent,
		Topic:           record.Topic,
		Name:            record.Name,
		Source:          record.Source,
		Queue:           record.Queue,
		State:           record.State,
		ResponseTo:      record.ResponseTo,
		AgentCorrelator: record.AgentCorrelator,
		Parameters:      record.Parameters,
		Payload:         record.Payload,
		ErrorString:     record.ErrorString,
		CreatedOn:       record.CreatedOn,
	}
}

func (record wireRecord) model() mid.Record {
	return mid.Record{
		SysID:           record.SysID,
		Agent:           record.Agent,
		Topic:           record.Topic,
		Name:            record.Name,
		Source:          record.Source,
		Queue:           record.Queue,
		State:           record.State,
		ResponseTo:      record.ResponseTo,
		AgentCorrelator: record.AgentCorrelator,
		Parameters:      record.Parameters,
		Payload:         record.Payload,
		ErrorString:     record.ErrorString,
		CreatedOn:       record.CreatedOn,
	}
}

type getRecordsEnvelope struct {
	XMLName xml.Name               `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`
	Body    getRecordsResponseBody `xml:"Body"`
}

type getRecordsResponseBody struct {
	Response struct {
		Records []wireRecord `xml:"getRecordsResult"`
	} `xml:"getRecordsResponse"`
}

type mutationEnvelope struct {
	XMLName xml.Name     `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`
	Body    mutationBody `xml:"Body"`
}

type mutationBody struct {
	Update *mutationResponse `xml:"updateResponse,omitempty"`
	Insert *mutationResponse `xml:"insertResponse,omitempty"`
}

type mutationResponse struct {
	SysID string `xml:"sys_id"`
}

type faultEnvelope struct {
	XMLName xml.Name  `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`
	Body    faultBody `xml:"Body"`
}

type faultBody struct {
	Fault struct {
		Code   string `xml:"faultcode"`
		String string `xml:"faultstring"`
	} `xml:"Fault"`
}

func writeFault(writer http.ResponseWriter, status int, code, message string) {
	response := faultEnvelope{}
	response.Body.Fault.Code = code
	response.Body.Fault.String = message
	writeXML(writer, status, response)
}

func writeXML(writer http.ResponseWriter, status int, value any) {
	encoded, err := xml.Marshal(value)
	if err != nil {
		http.Error(writer, "fixture encoding failure", http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/xml; charset=utf-8")
	writer.WriteHeader(status)
	_, _ = writer.Write(append([]byte(xml.Header), encoded...))
}
