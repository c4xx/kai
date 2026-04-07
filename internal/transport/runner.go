// Package transport wraps the Anthropic SDK in an AgentRunner interface.
// This is the ONLY file that imports github.com/anthropics/anthropic-sdk-go.
// All other packages use the AgentRunner interface.
package transport

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Tool is the interface every tool must implement.
type Tool interface {
	Name() string
	Description() string
	InputSchema() map[string]any
	Execute(ctx context.Context, params json.RawMessage) (string, error)
}

// RunResult contains the final output and token usage of an agent run.
type RunResult struct {
	FinalText  string
	TokensUsed int64
}

// AgentRunner drives the full agentic loop until end_turn or error.
type AgentRunner interface {
	RunToCompletion(ctx context.Context, systemPrompt string, tools []Tool, userMsg string) (*RunResult, error)
}

// betaRunner implements AgentRunner using the Anthropic SDK.
type betaRunner struct {
	client      anthropic.Client
	model       anthropic.Model
	maxTokens   int64
	tokenBudget int64 // max tokens for the whole run (0 = unlimited)
}

// NewBetaRunner creates an AgentRunner backed by the Anthropic API.
// tokenBudget is the maximum total tokens for the run (0 = unlimited).
func NewBetaRunner(apiKey string, model anthropic.Model, maxTokens, tokenBudget int64) AgentRunner {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &betaRunner{
		client:      client,
		model:       model,
		maxTokens:   maxTokens,
		tokenBudget: tokenBudget,
	}
}

// RunToCompletion drives the agentic loop: send message → handle tool_use blocks
// → send tool results → repeat until end_turn.
func (r *betaRunner) RunToCompletion(ctx context.Context, systemPrompt string, tools []Tool, userMsg string) (*RunResult, error) {
	betaTools := buildBetaTools(tools)
	toolMap := make(map[string]Tool, len(tools))
	for _, t := range tools {
		toolMap[t.Name()] = t
	}

	messages := []anthropic.BetaMessageParam{
		{
			Role:    anthropic.BetaMessageParamRoleUser,
			Content: []anthropic.BetaContentBlockParamUnion{anthropic.NewBetaTextBlock(userMsg)},
		},
	}

	var totalTokens int64
	var finalText string

	for {
		if r.tokenBudget > 0 && totalTokens >= r.tokenBudget {
			return &RunResult{FinalText: finalText, TokensUsed: totalTokens},
				fmt.Errorf("token budget exhausted (%d/%d used)", totalTokens, r.tokenBudget)
		}

		params := anthropic.BetaMessageNewParams{
			Model:     r.model,
			MaxTokens: r.maxTokens,
			Messages:  messages,
		}
		if systemPrompt != "" {
			params.System = []anthropic.BetaTextBlockParam{
				{Text: systemPrompt},
			}
		}
		if len(betaTools) > 0 {
			params.Tools = betaTools
		}

		resp, err := r.client.Beta.Messages.New(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("anthropic API: %w", err)
		}

		totalTokens += resp.Usage.InputTokens + resp.Usage.OutputTokens

		// Collect text from response.
		for _, block := range resp.Content {
			if block.Type == "text" {
				finalText = block.AsText().Text
			}
		}

		if resp.StopReason != anthropic.BetaStopReasonToolUse {
			// end_turn, max_tokens, stop_sequence — done.
			break
		}

		// Dispatch all tool_use blocks in parallel.
		type toolResult struct {
			useID  string
			output string
			isErr  bool
		}
		var (
			mu      sync.Mutex
			results []toolResult
			wg      sync.WaitGroup
		)

		for _, block := range resp.Content {
			if block.Type != "tool_use" {
				continue
			}
			tu := block.AsToolUse()
			wg.Add(1)
			go func(useID, name string, input any) {
				defer wg.Done()
				// Marshal input back to JSON for the Tool interface.
				raw, marshalErr := json.Marshal(input)
				tool, ok := toolMap[name]
				var output string
				var isErr bool
				if marshalErr != nil {
					output = fmt.Sprintf("marshal tool input: %v", marshalErr)
					isErr = true
				} else if !ok {
					output = fmt.Sprintf("unknown tool: %s", name)
					isErr = true
				} else {
					var execErr error
					output, execErr = tool.Execute(ctx, raw)
					if execErr != nil {
						output = execErr.Error()
						isErr = true
					}
				}
				mu.Lock()
				results = append(results, toolResult{useID, output, isErr})
				mu.Unlock()
			}(tu.ID, tu.Name, tu.Input)
		}
		wg.Wait()

		// Append assistant turn (all content blocks) to messages.
		// Convert resp.Content ([]BetaContentBlockUnion) to BetaContentBlockParamUnion.
		var assistantContent []anthropic.BetaContentBlockParamUnion
		for _, block := range resp.Content {
			switch block.Type {
			case "text":
				assistantContent = append(assistantContent, anthropic.NewBetaTextBlock(block.AsText().Text))
			case "tool_use":
				tu := block.AsToolUse()
				assistantContent = append(assistantContent, anthropic.NewBetaToolUseBlock(tu.ID, tu.Input, tu.Name))
			}
		}
		messages = append(messages, anthropic.BetaMessageParam{
			Role:    anthropic.BetaMessageParamRoleAssistant,
			Content: assistantContent,
		})

		// Build user turn with all tool results.
		var resultBlocks []anthropic.BetaContentBlockParamUnion
		for _, res := range results {
			resultBlocks = append(resultBlocks, anthropic.NewBetaToolResultBlock(res.useID, res.output, res.isErr))
		}
		messages = append(messages, anthropic.BetaMessageParam{
			Role:    anthropic.BetaMessageParamRoleUser,
			Content: resultBlocks,
		})
	}

	return &RunResult{
		FinalText:  finalText,
		TokensUsed: totalTokens,
	}, nil
}

// buildBetaTools converts Tool interface to anthropic.BetaToolUnionParam slice.
func buildBetaTools(tools []Tool) []anthropic.BetaToolUnionParam {
	params := make([]anthropic.BetaToolUnionParam, len(tools))
	for i, t := range tools {
		schema := t.InputSchema()
		schemaBytes, _ := json.Marshal(schema)
		desc := t.Description()
		tp := anthropic.BetaToolUnionParamOfTool(
			anthropic.BetaToolInputSchemaParam{Properties: schemaBytes},
			t.Name(),
		)
		tp.OfTool.Description = anthropic.String(desc)
		params[i] = tp
	}
	return params
}
