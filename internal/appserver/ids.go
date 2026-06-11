package appserver

import (
	"encoding/json"
)

func ParseThreadID(raw json.RawMessage) string {
	return extractThreadID(raw)
}

func ParseTurnID(raw json.RawMessage) string {
	return extractTurnID(raw)
}

func extractTaskID(raw json.RawMessage) string {
	var payload struct {
		TaskID string `json:"taskId"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return payload.TaskID
}

func extractThreadID(raw json.RawMessage) string {
	var payload struct {
		ThreadID string `json:"threadId"`
		Thread   struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	if payload.Thread.ID != "" {
		return payload.Thread.ID
	}
	return payload.ThreadID
}

func extractTurnID(raw json.RawMessage) string {
	var payload struct {
		TurnID string `json:"turnId"`
		Turn   struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	if payload.Turn.ID != "" {
		return payload.Turn.ID
	}
	return payload.TurnID
}
