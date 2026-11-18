package uucapture

import (
	"bytes"
	"encoding/json/v2"
	"strconv"
	"strings"
	"time"
)

const Process = "UURemoteServer"

type Kind int

const (
	DisplayDescribed Kind = iota + 1
	CaptureStarted
	CaptureStopped
	EncoderCreated
	EncoderEnded
	EncoderProbeEnded
)

func (k Kind) String() string {
	switch k {
	case DisplayDescribed:
		return "display described"
	case CaptureStarted:
		return "capture started"
	case CaptureStopped:
		return "capture stopped"
	case EncoderCreated:
		return "encoder created"
	case EncoderEnded:
		return "encoder ended"
	case EncoderProbeEnded:
		return "encoder probe ended"
	}
	return "unknown"
}

type Record struct {
	Time    time.Time
	Kind    Kind
	Display uint32
	PID     int32
}

var Keywords = []string{
	"SCDisplay initWithDict:",
	"CaptureWithCompletionHandler:]",
	"Stopping stream due to",
	"VTCompressionSession",
}

func Predicate() string {
	terms := make([]string, len(Keywords))
	for i, k := range Keywords {
		terms[i] = `eventMessage CONTAINS "` + k + `"`
	}
	return `process == "` + Process + `" AND (` + strings.Join(terms, " OR ") + ")"
}

const timeLayout = "2006-01-02 15:04:05.999999-0700"

func Parse(line []byte) (Record, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || line[0] != '{' {
		return Record{}, false
	}
	var entry struct {
		Message   string `json:"eventMessage"`
		Timestamp string `json:"timestamp"`
		Image     string `json:"processImagePath"`
		PID       int64  `json:"processID"`
	}
	if json.Unmarshal(line, &entry) != nil || entry.Message == "" {
		return Record{}, false
	}
	if entry.Image != "" && !strings.HasSuffix(entry.Image, "/"+Process) {
		return Record{}, false
	}
	at, err := time.Parse(timeLayout, entry.Timestamp)
	if err != nil {
		return Record{}, false
	}
	kind, display, ok := classify(entry.Message)
	if !ok {
		return Record{}, false
	}
	return Record{Time: at, Kind: kind, Display: display, PID: int32(entry.PID)}, true
}

func classify(msg string) (Kind, uint32, bool) {
	switch {
	case strings.Contains(msg, "SCDisplay initWithDict:"):
		id, ok := displayID(msg)
		return DisplayDescribed, id, ok
	case strings.Contains(msg, "startCaptureWithCompletionHandler:]"):
		return CaptureStarted, 0, true
	case strings.Contains(msg, "stopCaptureWithCompletionHandler:]:"),
		strings.Contains(msg, "Stopping stream due to"):
		return CaptureStopped, 0, true
	case strings.Contains(msg, "VTCompressionSessionCreate"):
		return EncoderCreated, 0, true
	case strings.Contains(msg, "VTCompressionSessionInvalidate"):
		if summary(msg) {
			return EncoderEnded, 0, true
		}
		return EncoderProbeEnded, 0, true
	}
	return 0, 0, false
}

func displayID(msg string) (uint32, bool) {
	_, rest, ok := strings.Cut(msg, "displayID=")
	if !ok {
		return 0, false
	}
	end := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
	if end >= 0 {
		rest = rest[:end]
	}
	id, err := strconv.ParseUint(rest, 10, 32)
	return uint32(id), err == nil && id > 0
}

func summary(msg string) bool {
	_, rest, ok := strings.Cut(msg, "encoded ")
	if !ok {
		return false
	}
	numbers := strings.FieldsFunc(rest, func(r rune) bool { return r < '0' || r > '9' })
	return len(numbers) >= 3
}
