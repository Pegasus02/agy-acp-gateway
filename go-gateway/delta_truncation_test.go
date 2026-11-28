package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDeltaPrompt_ResumedDeltaWithHugeOldToolResultPreservesLatestUserMessage(t *testing.T) {
	minifiedJS := "/*HEAD_MARKER*/" + strings.Repeat("var a=function(e,t){return e+t;};", 4200) + "/*TAIL_MARKER*/"
	if len(minifiedJS) < 130*1024 {
		t.Fatalf("expected >130KB fixture, got %d", len(minifiedJS))
	}

	var unseen []map[string]any
	unseen = append(unseen, map[string]any{
		"role": "user",
		"content": []any{
			map[string]any{
				"type":        "tool_result",
				"tool_use_id": "toolu_old_grep_01",
				"content":     minifiedJS,
			},
		},
	})
	unseen = append(unseen, map[string]any{
		"role":    "assistant",
		"content": "我刚刚 grep 扫到了一个压缩 JS 文件。",
	})
	// Add 180 intermediate turns to mimic the 207 -> 396 resumed_delta replay.
	for i := 0; i < 90; i++ {
		unseen = append(unseen,
			map[string]any{"role": "user", "content": fmt.Sprintf("历史问题 #%03d: %s", i, strings.Repeat("细节说明", 80))},
			map[string]any{"role": "assistant", "content": fmt.Sprintf("历史回复 #%03d: %s", i, strings.Repeat("处理结果", 80))},
		)
	}
	latestPrompt := "帮我更新一下 moreplay"
	unseen = append(unseen, map[string]any{
		"role":    "user",
		"content": latestPrompt,
	})

	blocks, err := resolveAndBuildPromptBlocks(context.Background(), GlobalImageFetcher, unseen, false, unseen, MaxTranscriptChar, ModelOrDefault("gemini-4-neon"))
	if err != nil {
		t.Fatalf("resolveAndBuildPromptBlocks failed: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 text block, got %d", len(blocks))
	}
	text, _ := blocks[0]["text"].(string)
	if !utf8.ValidString(text) {
		t.Fatalf("delta prompt text is not valid UTF-8")
	}
	if len(text) > deltaMaxTotalBytes {
		t.Fatalf("expected delta prompt <= %d bytes, got %d", deltaMaxTotalBytes, len(text))
	}
	if !strings.HasSuffix(text, "[USER]\n"+latestPrompt) {
		t.Fatalf("expected latest user message preserved verbatim at tail, got tail=%q", text[max(0, len(text)-120):])
	}
	if !strings.Contains(text, "omitted to preserve latest user turn") {
		t.Fatalf("expected omission marker for dropped earlier delta messages")
	}
	if !strings.Contains(text, "历史回复 #089") {
		t.Fatalf("expected most recent historical turn before latest user message to be retained")
	}
}

func TestDeltaPrompt_SingleHugeToolResultFollowedByLatestUserWithoutAssistantBoundary(t *testing.T) {
	hugeToolResult := "START_OF_LOG\n" + strings.Repeat("中文日志输出line\n", 10000) + "END_OF_LOG"
	latestPrompt := "请总结一下刚才的报错并帮我修复"

	unseen := []map[string]any{
		{
			"role": "user",
			"content": []any{
				map[string]any{
					"type":        "tool_result",
					"tool_use_id": "toolu_huge_02",
					"content":     hugeToolResult,
				},
			},
		},
		{
			"role":    "user",
			"content": latestPrompt,
		},
	}

	blocks, err := resolveAndBuildPromptBlocks(context.Background(), GlobalImageFetcher, unseen, false, unseen, MaxTranscriptChar, ModelOrDefault("gemini-4-neon"))
	if err != nil {
		t.Fatalf("resolveAndBuildPromptBlocks failed: %v", err)
	}
	text, _ := blocks[0]["text"].(string)
	if !utf8.ValidString(text) {
		t.Fatalf("output must remain valid UTF-8")
	}
	if len(text) > deltaMaxTotalBytes {
		t.Fatalf("expected folded output <= %d bytes, got %d", deltaMaxTotalBytes, len(text))
	}
	if !strings.Contains(text, "START_OF_LOG") || !strings.Contains(text, "END_OF_LOG") {
		t.Fatalf("expected folded historical tool_result to retain head and tail markers")
	}
	if !strings.Contains(text, "omitted") {
		t.Fatalf("expected middle omission marker in folded tool_result")
	}
	if !strings.HasSuffix(text, "[USER]\n"+latestPrompt) {
		t.Fatalf("expected latest user message intact at end, got tail=%q", text[max(0, len(text)-120):])
	}
}

func TestDeltaPrompt_OversizedLatestUserTurnStillPreservedIntact(t *testing.T) {
	oldBlob := strings.Repeat("OLD_HISTORY_", 10000)
	latestHuge := "LATEST_HEAD_" + strings.Repeat("payload-", 7000) + "_LATEST_TAIL"

	unseen := []map[string]any{
		{"role": "user", "content": oldBlob},
		{"role": "assistant", "content": "old ack"},
		{"role": "user", "content": latestHuge},
	}

	blocks, err := resolveAndBuildPromptBlocks(context.Background(), GlobalImageFetcher, unseen, false, unseen, MaxTranscriptChar, ModelOrDefault("gemini-4-neon"))
	if err != nil {
		t.Fatalf("resolveAndBuildPromptBlocks failed: %v", err)
	}
	text, _ := blocks[0]["text"].(string)
	if !strings.Contains(text, latestHuge) {
		t.Fatalf("latest user message must never be truncated even when > deltaMaxTotalBytes")
	}
	if !strings.Contains(text, "omitted to preserve latest user turn") {
		t.Fatalf("expected earlier history to be dropped with omission summary")
	}
}

func TestDeltaPrompt_ShortStickyDeltaByteIdentical(t *testing.T) {
	// Single-message sticky_delta
	single := []map[string]any{
		{"role": "user", "content": "帮我更新一下 moreplay"},
	}
	blocks, err := resolveAndBuildPromptBlocks(context.Background(), GlobalImageFetcher, single, false, single, MaxTranscriptChar, ModelOrDefault("gemini-4-neon"))
	if err != nil {
		t.Fatalf("single sticky_delta failed: %v", err)
	}
	if got, _ := blocks[0]["text"].(string); got != "帮我更新一下 moreplay" {
		t.Fatalf("single sticky_delta altered: got %q", got)
	}

	// Short multi-message delta (well under budget)
	multi := []map[string]any{
		{"role": "user", "content": "第一句"},
		{"role": "assistant", "content": "收到"},
		{"role": "user", "content": "第二句"},
	}
	blocks2, err := resolveAndBuildPromptBlocks(context.Background(), GlobalImageFetcher, multi, false, multi, MaxTranscriptChar, ModelOrDefault("gemini-4-neon"))
	if err != nil {
		t.Fatalf("short multi delta failed: %v", err)
	}
	want := "[USER]\n第一句\n\n[ASSISTANT]\n收到\n\n[USER]\n第二句"
	if got, _ := blocks2[0]["text"].(string); got != want {
		t.Fatalf("short multi delta altered:\n got: %q\nwant: %q", got, want)
	}
}

func TestRolloverPrompt_HugeHistoryCappedUnderAgy192KCeilingPreservingRecentUserAndCurrentTurn(t *testing.T) {
	// Reproduce production PARKED_IMAGE_ROLLOVER / compaction_rollover with ~360KB history:
	// early scientific discussion + recent user instruction ("我说用方案A做ppt") + current tool_result.
	var all []map[string]any
	all = append(all, map[string]any{"role": "user", "content": "OPENING_CONTEXT: 结合文献调研，讨论这两条路径的可行性"})
	for i := 0; i < 60; i++ {
		all = append(all,
			map[string]any{"role": "user", "content": fmt.Sprintf("旧文献段落 #%02d: %s", i, strings.Repeat("Fe(CO)5 intercalation Bi2Se3 superlattice ", 80))},
			map[string]any{"role": "assistant", "content": fmt.Sprintf("旧分析 #%02d: %s", i, strings.Repeat("扩散距离与热力学分析结果 ", 80))},
		)
	}
	all = append(all,
		map[string]any{"role": "user", "content": "我说用方案A做ppt"},
		map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "acp_slide_14", "name": "univer_execute", "input": map[string]any{"page": 14}}}},
		map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": "acp_slide_14", "content": "Screenshot page 14: [\"/private/tmp/v8_p14_check/page-14.png\"]"}}},
	)

	blocks, err := resolveAndBuildPromptBlocks(context.Background(), GlobalImageFetcher, all[len(all)-1:], true, all, MaxTranscriptChar, ModelOrDefault("gemini-4-neon"))
	if err != nil {
		t.Fatalf("resolveAndBuildPromptBlocks(rollover) failed: %v", err)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks (resource + text), got %d", len(blocks))
	}
	resMap, _ := blocks[0]["resource"].(map[string]any)
	histText, _ := resMap["text"].(string)
	curText, _ := blocks[1]["text"].(string)

	if !utf8.ValidString(histText) || !utf8.ValidString(curText) {
		t.Fatal("rollover prompt blocks must remain valid UTF-8")
	}
	combinedLen := len(histText) + len(curText)
	if combinedLen > rolloverMaxTotalBytes {
		t.Fatalf("combined rollover text (%d bytes) exceeds safe cap (%d bytes) and risks agy 192KB tail truncation", combinedLen, rolloverMaxTotalBytes)
	}
	if !strings.Contains(histText, "OPENING_CONTEXT") {
		t.Fatal("expected opening context preserved in rollover head")
	}
	if !strings.Contains(histText, "trimmed to stay within the model context window") {
		t.Fatal("expected middle trim marker in oversized rollover history")
	}
	if !strings.Contains(histText, "我说用方案A做ppt") {
		t.Fatal("expected recent user instruction ('我说用方案A做ppt') preserved in rollover tail")
	}
	if !strings.Contains(curText, "page-14.png") {
		t.Fatal("expected current turn tool_result preserved intact in rollover text block")
	}
}
