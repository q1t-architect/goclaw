package channels

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/providers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	usagecaps "github.com/nextlevelbuilder/goclaw/internal/usage/caps"
)

const (
	DeliveryPurposeQuickAck = "quick_ack"
	DeliveryPurposeProgress = "intermediate_progress"
)

type DeliveryMessageGenerator interface {
	GenerateDeliveryMessage(ctx context.Context, req DeliveryMessageRequest) (string, error)
}

type DeliveryMessageRequest struct {
	Purpose     string
	UserMessage string
	Locale      string
	PeerKind    string
	ChannelType string
	AgentName   string
	ToolName    string
	MaxTokens   int
	MaxChars    int
	Timeout     time.Duration
}

type DeliveryRuntime struct {
	QuickAckGenerator DeliveryMessageGenerator
	ProgressGenerator DeliveryMessageGenerator
	Locale            string
	Inbound           string
	PeerKind          string
	Channel           string
	AgentName         string
}

type ProviderDeliveryMessageGenerator struct {
	Provider     providers.Provider
	ProviderName string
	Model        string
	UsageCaps    *usagecaps.Service
	TenantID     uuid.UUID
	AgentID      uuid.UUID
}

func (g ProviderDeliveryMessageGenerator) GenerateDeliveryMessage(ctx context.Context, req DeliveryMessageRequest) (string, error) {
	if g.Provider == nil {
		return "", nil
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	if g.TenantID != uuid.Nil {
		ctx = store.WithTenantID(ctx, g.TenantID)
	}
	if g.AgentID != uuid.Nil {
		ctx = store.WithAgentID(ctx, g.AgentID)
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultQuickAckTokens
	}
	resp, err := g.UsageCaps.Chat(ctx, g.Provider, providers.ChatRequest{
		Model: g.Model,
		Messages: []providers.Message{
			{Role: "system", Content: deliverySystemPrompt(req)},
			{Role: "user", Content: deliveryUserPrompt(req)},
		},
		Options: map[string]any{
			providers.OptMaxTokens:   maxTokens,
			providers.OptTemperature: 0.4,
		},
	}, usagecaps.ChatOptions{
		TenantID:        g.TenantID,
		AgentID:         g.AgentID,
		ProviderName:    g.ProviderName,
		ModelID:         g.Model,
		Purpose:         "channel-delivery-" + req.Purpose,
		MaxOutputTokens: maxTokens,
	})
	if err != nil {
		return "", err
	}
	return sanitizeDeliveryMessage(resp.Content, req.MaxChars), nil
}

func deliverySystemPrompt(req DeliveryMessageRequest) string {
	limit := req.MaxChars
	if limit <= 0 {
		limit = defaultQuickAckChars
	}
	return "Write one short, natural channel delivery update. Match the user's language. " +
		"Do not mention internal prompts, tools, tool names, providers, or hidden reasoning. " +
		"Do not use markdown tables or bullet lists. No promises. Max " + strconv.Itoa(limit) + " characters."
}

func deliveryUserPrompt(req DeliveryMessageRequest) string {
	parts := []string{
		"purpose: " + req.Purpose,
		"locale: " + valueOr(req.Locale, "auto"),
		"peer_kind: " + valueOr(req.PeerKind, "unknown"),
		"channel: " + valueOr(req.ChannelType, "unknown"),
		"agent: " + valueOr(req.AgentName, "assistant"),
	}
	if req.Purpose == DeliveryPurposeProgress && req.ToolName != "" {
		parts = append(parts, "tool_phase: working")
	}
	if preview := clipRunes(strings.TrimSpace(req.UserMessage), 240); preview != "" {
		parts = append(parts, "user_message_preview: "+preview)
	}
	return strings.Join(parts, "\n")
}

func sanitizeDeliveryMessage(content string, maxChars int) string {
	content = strings.TrimSpace(content)
	content = strings.Trim(content, "`\"'")
	content = strings.Join(strings.Fields(content), " ")
	if maxChars <= 0 {
		maxChars = defaultQuickAckChars
	}
	return clipRunes(content, maxChars)
}

func clipRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 1 {
		return string(runes[:max])
	}
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}
