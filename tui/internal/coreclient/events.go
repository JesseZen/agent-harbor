package coreclient

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/asheshgoplani/agent-deck/internal/backend"
	"github.com/asheshgoplani/agent-deck/internal/coreclient/generated"
)

const maxRuntimeEventLineBytes = 1024 * 1024

func (client *Client) WatchInvalidations(ctx context.Context, cursor string) (<-chan backend.Invalidation, error) {
	lastID := uint64(0)
	if cursor != "" {
		parsed, err := strconv.ParseUint(cursor, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid runtime event cursor %q", cursor)
		}
		lastID = parsed
	}
	params := &generated.StreamRuntimeEventsParams{}
	if cursor != "" {
		params.LastEventID = &cursor
	}
	response, err := client.api.StreamRuntimeEvents(ctx, params)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		status := response.StatusCode
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
		_ = response.Body.Close()
		return nil, &APIError{Operation: "streamRuntimeEvents", StatusCode: status}
	}

	events := make(chan backend.Invalidation)
	go client.readInvalidations(ctx, response.Body, lastID, events)
	return events, nil
}

func (client *Client) readInvalidations(ctx context.Context, body io.ReadCloser, lastID uint64, destination chan<- backend.Invalidation) {
	defer close(destination)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 4096), maxRuntimeEventLineBytes)
	var eventID, eventType string
	var data strings.Builder
	emit := func() bool {
		if eventType == "" && eventID == "" && data.Len() == 0 {
			return true
		}
		if eventType == "resync_required" {
			return sendInvalidation(ctx, destination, backend.Invalidation{EventID: eventID, Type: eventType})
		}
		parsedID, err := strconv.ParseUint(eventID, 10, 64)
		if err != nil || parsedID <= lastID {
			return sendInvalidation(ctx, destination, backend.Invalidation{Err: fmt.Errorf("invalid runtime event ID %q", eventID)})
		}
		var event generated.RuntimeEvent
		if err := json.Unmarshal([]byte(data.String()), &event); err != nil {
			return sendInvalidation(ctx, destination, backend.Invalidation{Err: fmt.Errorf("decode runtime event: %w", err)})
		}
		if event.InstanceId != client.expectedInstanceID || event.Id != eventID || string(event.Type) != eventType {
			return sendInvalidation(ctx, destination, backend.Invalidation{Err: ErrInstanceMismatch})
		}
		lastID = parsedID
		return sendInvalidation(ctx, destination, backend.Invalidation{EventID: eventID, Type: eventType})
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if !emit() {
				return
			}
			eventID, eventType = "", ""
			data.Reset()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			eventID = value
		case "event":
			eventType = value
		case "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		_ = sendInvalidation(ctx, destination, backend.Invalidation{Err: fmt.Errorf("runtime event stream: %w", err)})
	}
}

func sendInvalidation(ctx context.Context, destination chan<- backend.Invalidation, event backend.Invalidation) bool {
	select {
	case destination <- event:
		return event.Err == nil
	case <-ctx.Done():
		return false
	}
}
