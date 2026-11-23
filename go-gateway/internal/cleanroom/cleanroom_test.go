package cleanroom

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"unicode"
)

func TestReversibleCodecAndChunkSplitRestorer(t *testing.T) {
	codec := NewReversibleSessionCodec("tenant-key-1")
	orig := `<claude_custom id="x">Keep /Users/dev/DeepSeek Harness/app intact</claude_custom>`
	encoded := codec.EncodeOutboundText(orig)
	if strings.Contains(encoded, "claude_custom") || strings.Contains(encoded, "DeepSeek Harness") {
		t.Fatalf("expected encoded output to mask wrapper tag & phrase, got %q", encoded)
	}
	for split := 1; split <= 13; split++ {
		sr := codec.NewStreamRestorer()
		var out strings.Builder
		for i := 0; i < len(encoded); i += split {
			end := i + split
			if end > len(encoded) {
				end = len(encoded)
			}
			out.WriteString(sr.Push(encoded[i:end]))
		}
		out.WriteString(sr.Flush())
		if out.String() != orig {
			t.Fatalf("split=%d mismatch: got %q want %q", split, out.String(), orig)
		}
	}
}

func TestPruneSystemPromptAndOutboundSanitization(t *testing.T) {
	in := "x-anthropic-billing-header: foo\nYou are Claude Code, Anthropic's official CLI for Claude.\nPreserve ~/repo/claude_runner.py when helping Claude Code users."
	out := PruneClientSystemPrompt(in, nil, nil)
	if strings.Contains(out, "Claude Code") || strings.Contains(out, "x-anthropic-billing-header") {
		t.Fatalf("client identity or billing header leaked: %q", out)
	}
	if !strings.Contains(out, "~/repo/claude_runner.py") {
		t.Fatalf("masked file path lost during sanitization: %q", out)
	}
}

func TestTosBreakerFileMode0600AndWafRuleCache(t *testing.T) {
	dir := t.TempDir()
	tosPath := filepath.Join(dir, "tos-tripped")
	var trips atomic.Int32
	brk := NewTosBreaker(tosPath, func() { trips.Add(1) })
	if err := brk.Trip("violation of Terms of Service"); err != nil {
		t.Fatalf("Trip error: %v", err)
	}
	if trips.Load() != 1 {
		t.Fatalf("expected 1 onTrip callback, got %d", trips.Load())
	}
	info, err := os.Stat(tosPath)
	if err != nil {
		t.Fatalf("Stat tosPath error: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("expected 0600 mode, got %#o", info.Mode().Perm())
	}
	if tripped, _ := brk.IsTripped(); !tripped {
		t.Fatal("expected IsTripped()==true")
	}

	rulesPath := filepath.Join(dir, "waf.json")
	if err := PersistRuleFile(rulesPath, "blocked exact phrase longer than twelve chars", "bisect", nil); err != nil {
		t.Fatalf("PersistRuleFile error: %v", err)
	}
	var cache WafRuleCache
	rules := cache.Load(rulesPath)
	if len(rules) != 1 {
		t.Fatalf("expected 1 loaded rule, got %d", len(rules))
	}
	rewritten := ApplyRules("hello\n- blocked exact phrase longer than twelve chars\nworld", rules)
	if strings.Contains(rewritten, "blocked exact phrase") || strings.Contains(rewritten, "\n- \n") {
		t.Fatalf("unexpected WAF rule output: %q", rewritten)
	}
}

func refSanitizeOutboundTextUngated(s string, applyWafCtx func(string) string) string {
	if s == "" || strings.TrimSpace(s) == "" {
		return ""
	}
	leadIndent := leadingLine1HorizontalIndent(s)
	s = sysReminderOpenRe.ReplaceAllString(s, "<workspace-context>")
	s = sysReminderCloseRe.ReplaceAllString(s, "</workspace-context>")
	s = pastedContentOpenRe.ReplaceAllString(s, "<user-excerpt$1>")
	s = pastedContentCloseRe.ReplaceAllString(s, "</user-excerpt>")

	var masked []string
	var protBuf strings.Builder
	protBuf.Grow(len(s))
	lastPos := 0
	for _, loc := range maskTokenMasterRe.FindAllStringIndex(s, -1) {
		start, end := loc[0], loc[1]
		m := s[start:end]
		protBuf.WriteString(s[lastPos:start])
		if shouldSkipMaskingToken(m) || isSlashJoinedVendorList(s, start, end) {
			protBuf.WriteString(m)
		} else {
			idx := len(masked)
			masked = append(masked, m)
			protBuf.WriteString("\x00TOK_")
			protBuf.WriteString(strconv.Itoa(idx))
			protBuf.WriteByte(0)
		}
		lastPos = end
	}
	protBuf.WriteString(s[lastPos:])
	cleaned := protBuf.String()

	for _, item := range stageCPhraseReplacements {
		cleaned = item.re.ReplaceAllString(cleaned, item.repl)
	}

	if len(masked) > 0 {
		var b strings.Builder
		b.Grow(len(cleaned))
		for i := 0; i < len(cleaned); {
			if cleaned[i] == 0 && i+5 < len(cleaned) && cleaned[i:i+5] == "\x00TOK_" {
				end := strings.IndexByte(cleaned[i+5:], 0)
				if end > 0 {
					numStr := cleaned[i+5 : i+5+end]
					if idx, err := strconv.Atoi(numStr); err == nil && idx >= 0 && idx < len(masked) {
						b.WriteString(masked[idx])
						i = i + 5 + end + 1
						continue
					}
				}
			}
			b.WriteByte(cleaned[i])
			i++
		}
		cleaned = b.String()
	}
	if applyWafCtx != nil {
		cleaned = applyWafCtx(cleaned)
	}
	if strings.TrimSpace(cleaned) == "" {
		return ""
	}
	if leadIndent != "" && strings.HasPrefix(cleaned, leadIndent) {
		return strings.TrimRightFunc(cleaned, unicode.IsSpace)
	}
	return strings.TrimSpace(cleaned)
}

func refNeutralizeToolIdentifierUngated(name string) string {
	if name == "" {
		return ""
	}
	out := name
	for _, rep := range toolVendorTokenReplacements {
		out = rep.re.ReplaceAllString(out, rep.repl)
	}
	return out
}

func TestSanitizeOutboundTextStemGateEquivalence(t *testing.T) {
	waf := func(s string) string {
		return ApplyRulesInWorkspaceContext(s, []WafRewriteRule{{Literal: "secret trigger phrase"}})
	}
	samples := []string{
		"",
		"   \n\t  ",
		"Plain Go source code: func main() { fmt.Println(\"hello world\") }",
		"  indented code line without any vendor keywords\n\t/Users/dev/project/internal/pkg/service.go",
		"<workspace-context>\nsecret trigger phrase\nkeep this line\n</workspace-context>",
		"<system-reminder>internal note</system-reminder>\nKeep normal text.",
		"<pasted_content id=\"abc\">hello world</pasted_content>",
		"<system-reminder>unclosed\n<claude_instructions mode=\"strict\">hello</claude_instructions>\n<codex_tool name=\"x\"/>",
		"<anthropic_block>x</anthropic_block><openai_block>y</openai_block><cursor_rule>z</cursor_rule><cline_step>a</cline_step><roocode_cfg>b</roocode_cfg><dsh_env>c</dsh_env>",
		"Preserve ~/.claude/settings.json and https://docs.anthropic.com/claude-code while replacing Claude Code and Anthropic.",
		"CLAUDE CODE claude-code claude_code Claude.md CLAUDE.md Claude Sonnet 4.6 Claude Opus 4.7 Claude Haiku 4.5 Claude 3.7 claude-sonnet-4 claude-opus-4 claude-haiku-4",
		"Anthropic's official CLI for Claude, Anthropic API, ANTHROPIC_API_KEY, anthropic.com, support.claude.com, code.claude.com",
		"Anthropic / Claude model and claude-cli/2.1.305 plus x-claude-code-session-id",
		"OpenAI Codex CLI, Codex CLI, codex-cli, codex, AGENTS.md, ChatGPT, GPT-5, GPT-4o, o3-mini, OPENAI_API_KEY, openai.com",
		"DeepSeek Harness, deepseek-harness, DeepSeek CLI, DeepSeek-V3, DeepSeek-R1, DeepSeek, dsh-bridge, dsh_, DSH, window.__DSH_BOOT__",
		"Cursor Agent, Cursor IDE, .cursorrules, Windsurf, Roo Code, Roo-Code, Kilo Code, OpenCode, OpenClaw, ClawdBot, Augment Code, PI-Desktop, Cline",
		"  Claude Code with leading spaces\n\tCodex CLI with leading tab\n  DeepSeek Harness with indent\n",
		"Mixed edge: my-claude-file.txt /tmp/.claude/cache.db http://openai.com/docs/codex `CLAUDE.md` vs Claude Code.",
		"Non-matching partial substrings: applaud, claustrophobic, anthropometry, rooco, recline, cursorily.",
	}
	for i, in := range samples {
		gotNoWaf := SanitizeOutboundText(in, nil)
		wantNoWaf := refSanitizeOutboundTextUngated(in, nil)
		if gotNoWaf != wantNoWaf {
			t.Fatalf("sample[%d] (no WAF) mismatch:\n  in:   %q\n  got:  %q\n  want: %q", i, in, gotNoWaf, wantNoWaf)
		}
		gotWaf := SanitizeOutboundText(in, waf)
		wantWaf := refSanitizeOutboundTextUngated(in, waf)
		if gotWaf != wantWaf {
			t.Fatalf("sample[%d] (with WAF) mismatch:\n  in:   %q\n  got:  %q\n  want: %q", i, in, gotWaf, wantWaf)
		}
		// Verify sensitivePathPhraseRe stem gate never misses any regex match.
		if sensitivePathPhraseRe.MatchString(in) && !hasSensitivePathPhraseStem(in) {
			t.Fatalf("hasSensitivePathPhraseStem missed regex match in sample[%d]: %q", i, in)
		}
	}

	toolNames := []string{
		"", "Read", "Write", "Bash", "Grep", "mcp__git__status", "mcp__dbx__execute_query",
		"claude_code_run", "CLAUDE_EXEC", "ClaudeCodeRun", "anthropic_fetch", "codex_patch", "codex-cli-run", "openai_chat",
		"deepseek_harness_run", "DeepSeekHarnessExec", "dsh_shell", "run_dsh", "run_dsh_now",
		"openclaw_exec", "open_claw_run", "clawdbot_go", "clawd_bot_go",
		"roo_code_apply", "kilo_code_run", "augment_code_patch", "cursor_edit", "cline_task",
		"MixedAnthropicClaudeCodex", "my_dsh_bridge_tool", "non_vendor_tool_123",
	}
	for _, name := range toolNames {
		got := NeutralizeToolIdentifier(name)
		want := refNeutralizeToolIdentifierUngated(name)
		if got != want {
			t.Fatalf("NeutralizeToolIdentifier(%q) = %q, want %q", name, got, want)
		}
	}
}
