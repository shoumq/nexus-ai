package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"nexus/internal/dto"
	"nexus/internal/hepler"
	"regexp"
	"strings"
)

const (
	defaultHFURL   = "https://router.huggingface.co/v1/chat/completions"
	defaultHFModel = "deepseek-ai/DeepSeek-R1:cheapest"
)

func NewHFClient(cfg HFConfig) *HFClient {
	if cfg.URL == "" {
		cfg.URL = defaultHFURL
	}
	if cfg.Token == "" {
		cfg.Token = ""
	}
	if cfg.Model == "" {
		cfg.Model = defaultHFModel
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = hepler.SystemPromptRU
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}

	return &HFClient{
		url:        cfg.URL,
		token:      cfg.Token,
		model:      cfg.Model,
		system:     cfg.SystemPrompt,
		httpClient: cfg.HTTPClient,
	}
}

func (c *HFClient) CallInsight(ctx context.Context, p dto.HFPrompt) (string, error) {
	userPrompt := hepler.BuildRussianPrompt(p)

	text1, finish1, err := c.hfChatOnce(ctx, c.url, c.token, c.model, c.system, userPrompt, 1200)
	if err != nil {
		return "", err
	}
	text1 = toPlainText(text1)
	text1 = sanitizeInsight(text1, p)

	if isTruncated(finish1, text1) {
		contPrompt := fmt.Sprintf(hepler.ContinuePromptTmplRU, text1)

		text2, _, err2 := c.hfChatOnce(ctx, c.url, c.token, c.model, c.system, contPrompt, 900)
		if err2 == nil {
			text2 = toPlainText(text2)
			text2 = sanitizeInsight(text2, p)
			merged := strings.TrimSpace(text1 + "\n" + text2)
			if merged != "" {
				text1 = merged
			}
		}
	}

	if !validateInsight(text1, p) {
		rep := fmt.Sprintf(
			hepler.RepairPromptTmplRU,
			p.NumPoints,
			p.NumObservedHours,
			p.NumObservedWeekdays,
			p.ObservedHoursList,
			p.ObservedWeekdaysList,
			strings.Join(p.ProposedSchedule.BestFocusHours, ", "),
			strings.Join(p.ProposedSchedule.BestLightTasksHours, ", "),
			p.BurnoutLevel,
			text1,
		)

		fixed, _, err3 := c.hfChatOnce(ctx, c.url, c.token, c.model, c.system, rep, 1200)
		if err3 == nil {
			fixed = toPlainText(fixed)
			fixed = sanitizeInsight(fixed, p)
			if validateInsight(fixed, p) {
				return fixed, nil
			}
		}
	}

	if strings.TrimSpace(text1) == "" {
		return "", errors.New("hf empty content after cleaning")
	}
	return text1, nil
}

func (c *HFClient) hfChatOnce(ctx context.Context, url, token, model, system, user string, maxTokens int) (text string, finishReason string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	reqBody, _ := json.Marshal(dto.HfChatRequest{
		Model: model,
		Messages: []dto.HfChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		MaxTokens:   maxTokens,
		Temperature: 0.4,
		TopP:        0.9,
		Stream:      false,
	})

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var b bytes.Buffer
		_, _ = b.ReadFrom(resp.Body)
		return "", "", fmt.Errorf("hf status %d: %s", resp.StatusCode, b.String())
	}

	var out dto.HfChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", fmt.Errorf("hf decode error: %v", err)
	}
	if len(out.Choices) == 0 {
		return "", "", errors.New("hf empty response (no choices)")
	}

	t := strings.TrimSpace(out.Choices[0].Message.Content)
	fr := strings.TrimSpace(out.Choices[0].FinishReason)
	return t, fr, nil
}

func isTruncated(finishReason, text string) bool {
	if strings.EqualFold(finishReason, "length") {
		return true
	}

	if text == "" {
		return false
	}
	last := strings.TrimSpace(text)
	if strings.HasSuffix(last, ":") || strings.HasSuffix(last, "-") || strings.HasSuffix(last, "–") {
		return true
	}

	if len(last) >= 2 && last[len(last)-1] == ':' {
		return true
	}
	return false
}

func cleanLLMText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}

	low := strings.ToLower(s)

	if idxEnd := strings.LastIndex(low, "</think>"); idxEnd != -1 {
		after := strings.TrimSpace(s[idxEnd+len("</think>"):])
		if after != "" {
			return after
		}
	}

	s = strings.ReplaceAll(s, "<think>", "")
	s = strings.ReplaceAll(s, "</think>", "")
	s = strings.ReplaceAll(s, "<THINK>", "")
	s = strings.ReplaceAll(s, "</THINK>", "")

	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	s = strings.TrimLeft(s, "\n\r\t ")

	return strings.TrimSpace(s)
}

var (
	reBold       = regexp.MustCompile(`\*\*(.*?)\*\*`)
	reInlineCode = regexp.MustCompile("`([^`]*)`")
	reHeading    = regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s+`)
	reListNum    = regexp.MustCompile(`(?m)^\s*\d+\.\s+`)
	reListDash   = regexp.MustCompile(`(?m)^\s*[-•]\s+`)
	reMultiSpace = regexp.MustCompile(`[ \t]{2,}`)
)

func toPlainText(s string) string {
	s = cleanLLMText(s)

	s = reHeading.ReplaceAllString(s, "")
	s = reBold.ReplaceAllString(s, "$1")
	s = reInlineCode.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "__", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "_", "")

	s = reListNum.ReplaceAllString(s, "")
	s = reListDash.ReplaceAllString(s, "")

	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = reMultiSpace.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)

	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	empty := 0
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			empty++
			if empty > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		empty = 0
		out = append(out, ln)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func sanitizeInsight(text string, p dto.HFPrompt) string {
	t := strings.TrimSpace(text)

	bad := []string{
		"глюкоз", "гормон", "биоритм", "биолог", "физиолог", "в крови",
		"<think>", "</think>", "analysis", "thoughts",
	}

	lines := strings.Split(t, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		ll := strings.ToLower(ln)
		skip := false
		for _, b := range bad {
			if strings.Contains(ll, b) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, strings.TrimSpace(ln))
		}
	}
	t = strings.TrimSpace(strings.Join(out, "\n"))

	if p.NumPoints >= 5 && p.NumObservedHours >= 5 && p.NumObservedWeekdays >= 5 {
		t = removeLinesContaining(t, []string{"данных мало", "вывод предварител"})
	}

	t = strings.ReplaceAll(t, "\r\n", "\n")
	rows := strings.Split(t, "\n")
	final := make([]string, 0, len(rows))
	empty := 0
	for _, r := range rows {
		r = strings.TrimSpace(r)
		if r == "" {
			empty++
			if empty > 1 {
				continue
			}
			final = append(final, "")
			continue
		}
		empty = 0
		final = append(final, r)
	}
	return strings.TrimSpace(strings.Join(final, "\n"))
}

func removeLinesContaining(s string, needles []string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		ll := strings.ToLower(ln)
		skip := false
		for _, n := range needles {
			if strings.Contains(ll, n) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, ln)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func validateInsight(text string, p dto.HFPrompt) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}

	required := []string{"Энергия", "Выгорание", "Что делать завтра", "Что добавить в трекинг"}
	for _, h := range required {
		if !strings.Contains(t, "\n"+h+"\n") && !strings.HasPrefix(t, h+"\n") {
			return false
		}
	}

	needUnknown := "Риск выгорания пока неизвестен из-за недостатка данных."
	if p.BurnoutLevel == "unknown" {
		if !strings.Contains(t, needUnknown) {
			return false
		}
	} else {
		if strings.Contains(t, needUnknown) {
			return false
		}
	}

	if p.NumPoints >= 5 && p.NumObservedHours >= 5 && p.NumObservedWeekdays >= 5 {
		low := strings.ToLower(t)
		if strings.Contains(low, "данных мало") || strings.Contains(low, "вывод предварител") {
			return false
		}
	}

	banned := []string{"<think>", "</think>", "analysis", "thoughts"}
	low := strings.ToLower(t)
	for _, b := range banned {
		if strings.Contains(low, b) {
			return false
		}
	}

	block := extractBlock(t, "Что делать завтра", "Что добавить в трекинг")
	if strings.TrimSpace(block) == "" {
		return false
	}

	actions := splitActions(block)
	if len(actions) != 3 {
		return false
	}

	return true
}

func extractBlock(full, startTitle, endTitle string) string {
	start := strings.Index(full, "\n"+startTitle+"\n")
	if start == -1 {
		if strings.HasPrefix(full, startTitle+"\n") {
			start = 0
		} else {
			return ""
		}
	} else {
		start += len("\n" + startTitle + "\n")
	}

	end := strings.Index(full[start:], "\n"+endTitle+"\n")
	if end == -1 {
		return strings.TrimSpace(full[start:])
	}
	return strings.TrimSpace(full[start : start+end])
}

func splitActions(block string) []string {
	lines := strings.Split(block, "\n")
	out := make([]string, 0, 3)
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		out = append(out, ln)
	}
	if len(out) == 1 {
		parts := splitBySentence(out[0])
		out = out[:0]
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func splitBySentence(s string) []string {
	seps := []rune{'.', '!', '?'}
	var res []string
	cur := strings.Builder{}
	for _, r := range s {
		cur.WriteRune(r)
		for _, sep := range seps {
			if r == sep {
				part := strings.TrimSpace(cur.String())
				if part != "" {
					res = append(res, part)
				}
				cur.Reset()
				break
			}
		}
	}
	last := strings.TrimSpace(cur.String())
	if last != "" {
		res = append(res, last)
	}
	return res
}
