package datacircle

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	openai "github.com/sashabaranov/go-openai"
)

func TestDefinitions(t *testing.T) {
	tools := New(nil).Tools()
	if len(tools) != 4 || len(New(nil).Functions()) != 4 {
		t.Fatalf("%d tools, want 4", len(tools))
	}
	for i, tl := range tools {
		if tl.Type != openai.ToolTypeFunction || tl.Function == nil || tl.Function.Name != Names[i] {
			t.Fatalf("tool %d is %+v, want the function %s", i, tl, Names[i])
		}
		// As the client sends it: the tool in the request's tools.
		sent, _ := json.Marshal(openai.ChatCompletionRequest{Model: "m", Tools: []openai.Tool{tl}})
		name := tl.Function.Name
		switch name {
		case BestPlansName, ProviderName:
			if !strings.Contains(string(sent), `"required":["slug"]`) {
				t.Errorf("%s requires no slug: %s", name, sent)
			}
		default:
			if strings.Contains(string(sent), `"required"`) {
				t.Errorf("%s requires an input: %s", name, sent)
			}
		}
		if name == SearchPlansName && !strings.Contains(string(sent), `"enum":["score","provider","price"]`) {
			t.Errorf("%s's sort has no enum: %s", name, sent)
		}
		// Read as JSON: go-openai v1.42.0 and later write a tool's keys in another order.
		var req struct {
			Tools []struct {
				Type     string `json:"type"`
				Function struct{ Name, Description string }
			}
		}
		if json.Unmarshal(sent, &req) != nil || len(req.Tools) != 1 || req.Tools[0].Type != "function" ||
			req.Tools[0].Function.Name != name || req.Tools[0].Function.Description != abouts[name] ||
			!strings.Contains(string(sent), `"limit":{"description":"How many`) ||
			!strings.Contains(string(sent), `"parameters":{"properties":{`) || !strings.Contains(string(sent), `"type":"object"`) {
			t.Errorf("%s's definition: %s", name, sent)
		}
	}
}

func TestCalls(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		switch {
		case r.URL.Path == "/apis":
			w.Write([]byte(`[{"api":"Employee profile","slug":"linkedin-profile","sheetPage":"s"},{"api":"Employee email","slug":"email-finder","plans":3}]`))
		case r.URL.Path == "/apis/email-finder/best":
			w.Write([]byte(`{"api":"Employee email","plans":[{"provider":"A","rank":1,"terms":"long"}]}`))
		case r.URL.Path == "/plans" && (r.URL.Query().Get("api") == "Bright Data" || r.URL.Query().Get("api") == "bright-data"):
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"api must be one of: Employee email"}`))
		case r.URL.Path == "/plans" && strings.Contains(r.URL.Query().Get("query"), "-"):
			w.Write([]byte(`{"total":0,"plans":[]}`))
		case r.URL.Path == "/plans":
			w.Write([]byte(`{"total":1,"plans":[{"provider":"Bright Data","rank":2}]}`))
		case r.URL.Path == "/providers/bright-data":
			w.Write([]byte(`{"provider":"Bright Data","apis":[{"api":"Employee profile","plans":[{"plan":"C"}]},{"api":"Employee email","rank":4,"plans":[{"plan":"B","rank":9},{"plan":"A","rank":4}]}]}`))
		case r.URL.Path == "/providers/down":
			w.WriteHeader(502)
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"error":"no API has the slug nope"}`))
		}
	}))
	defer server.Close()
	tools := New(&Config{BaseURL: server.URL})
	ctx := context.Background()
	for _, call := range []struct {
		name, arguments string
		fails           bool
		has             string
	}{
		{ListAPIsName, `{"query":"email"}`, false, `"total":1`},
		{ListAPIsName, `{}`, false, `"total":2`},
		{BestPlansName, `{"slug":"Email Finder","limit":"2"}`, false, `"provider":"A"`},
		{SearchPlansName, `{"api":"Bright Data"}`, false, `No API is named`},
		{SearchPlansName, `{"api":"Bright Data","query":"growth"}`, true, `No API is named "Bright Data". datacircle_list_apis gives every API's name and slug.`},
		{SearchPlansName, `{"api":"bright-data","query":null}`, false, `these are the plans whose name holds \"bright data\".","plans":[{"provider":"Bright Data"`},
		{SearchPlansName, `{"query":"bright-data","sort":"price","limit":null}`, false, `"plans":[{"provider":"Bright Data"`},
		{ProviderName, `{"slug":"Bright Data"}`, false, `"apis":[{"api":"Employee email","best":{"plan":"A"`},
		{ProviderName, `{"slug":"bright-data","limit":1}`, false, `"ranked":1,"total":2`},
		{ProviderName, `{"slug":"bright-data","limit":1}`, false, `{"about":"Bright Data has plans on 2 APIs on Datacircle and a rank on 1 of them (1 is the best). Its best rank: 4, on Employee email.","apis":[`},
		{BestPlansName, `{"slug":"nope"}`, true, `no API has the slug nope. datacircle_list_apis gives every API's slug. A provider's plans: datacircle_search_plans`},
		{BestPlansName, `{"provider":"Bright Data","limit":2}`, true, `Give an API's slug, like email-finder: datacircle_list_apis gives every slug. A provider's plans:`},
		{ListAPIsName, `{"query":"Bright Data"}`, false, `"note":"No API's name holds every word of \"Bright Data\". A provider's plans: datacircle_search_plans`},
		{ProviderName, `{"slug":"down"}`, true, `datacircle: 502 Bad Gateway`},
		{BestPlansName, `{"slug":"email-finder","limit":[2]}`, true, `The arguments are no JSON object of this tool's parameters`},
		{ProviderName, `{"slug":"nope"}`, true, `no API has the slug nope. datacircle_search_plans with words of its name as query gives each plan's provider.`},
		{ListAPIsName, ``, false, `"total":2`},
		{"get_weather", `{}`, true, `datacircle: no tool is named "get_weather"`},
	} {
		got, err := tools.Run(ctx, call.name, call.arguments)
		if err != nil {
			got = err.Error()
		}
		if (err != nil) != call.fails || !strings.Contains(got, call.has) {
			t.Errorf("%s(%s) = %s, %v; want it to hold %s", call.name, call.arguments, got, err, call.has)
		}
		if strings.Contains(got, "terms") || strings.Contains(got, "sheetPage") {
			t.Errorf("%s gave a key it drops: %s", call.name, got)
		}
	}
	got, _ := tools.Run(ctx, ListAPIsName, `{"query":"employee email"}`)
	if strings.Contains(got, "note") {
		t.Errorf("datacircle_list_apis notes a query an API's name holds every word of: %s", got)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := tools.Run(canceled, ListAPIsName, `{}`); err != context.Canceled {
		t.Errorf("a canceled call gave %v", err)
	}
	if want := "/apis/email-finder/best?limit=2"; paths[2] != want {
		t.Errorf("called %s, want %s", paths[2], want)
	}
	if want := "/plans?query=bright+data&sort=price"; paths[10] != want {
		t.Errorf("called %s, want %s", paths[10], want)
	}

	// A chat completion's tool call into its tool message.
	msg, ok := tools.Call(ctx, openai.ToolCall{ID: "call_1", Type: openai.ToolTypeFunction,
		Function: openai.FunctionCall{Name: BestPlansName, Arguments: `{"slug":"email-finder"}`}})
	if !ok || msg.Role != openai.ChatMessageRoleTool || msg.ToolCallID != "call_1" || msg.Name != BestPlansName ||
		!strings.HasPrefix(msg.Content, `{"api":"Employee email","plans":[{"provider":"A","rank":1}]}`) {
		t.Errorf("Call gave %+v, %v", msg, ok)
	}
	msg, ok = tools.Call(ctx, openai.ToolCall{ID: "call_2", Function: openai.FunctionCall{Name: BestPlansName, Arguments: `{"slug":"nope"}`}})
	if !ok || msg.Content != `{"error":"no API has the slug nope. datacircle_list_apis gives every API's slug. A provider's plans: datacircle_search_plans with its name as query, like bright data, or datacircle_provider."}` {
		t.Errorf("Call of an unknown slug gave %+v, %v", msg, ok)
	}
	if _, ok := tools.Call(ctx, openai.ToolCall{ID: "call_3", Function: openai.FunctionCall{Name: "get_weather"}}); ok {
		t.Errorf("Call ran another app's tool")
	}

	// A Responses API output: its function calls of the four tools, decoded as the client decodes them, or as structs.
	var output []any
	json.Unmarshal([]byte(`[{"type":"reasoning","id":"r"},{"type":"function_call","call_id":"c1","name":"datacircle_list_apis","arguments":"{\"query\":\"email\"}"},
		{"type":"function_call","call_id":"c2","name":"get_weather","arguments":"{}"},{"type":"message","role":"assistant","content":[]}]`), &output)
	output = append(output, struct {
		Type      string `json:"type"`
		CallID    string `json:"call_id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}{"function_call", "c3", ProviderName, `{"slug":"down"}`})
	outs := tools.Outputs(ctx, output)
	sent, _ := json.Marshal(outs)
	if len(outs) != 2 || !strings.HasPrefix(string(sent), `[{"call_id":"c1","output":"{\"apis\":[{\"api\":\"Employee email\",\"plans\":3,\"slug\":\"email-finder\"}],\"total\":1}","type":"function_call_output"},{"call_id":"c3","output":"{\"error\":\"datacircle: 502 Bad Gateway\"}","type":"function_call_output"}]`) {
		t.Errorf("Outputs gave %s", sent)
	}
}

// TestLive calls each tool once on the API itself: DATACIRCLE_LIVE=1 go test -run Live.
func TestLive(t *testing.T) {
	if os.Getenv("DATACIRCLE_LIVE") == "" {
		t.Skip("DATACIRCLE_LIVE is unset")
	}
	tools := New(nil)
	ctx := context.Background()
	for _, call := range []struct{ name, arguments, has string }{
		{ListAPIsName, `{"query":"email finder","limit":3}`, `"slug":"email-finder"`},
		{BestPlansName, `{"slug":"email-finder","limit":3}`, `"rank":1`},
		{SearchPlansName, `{"query":"bright data","sort":"price","limit":3}`, `"provider":"Bright Data"`},
		{ProviderName, `{"slug":"Bright Data","limit":3}`, `"about":"Bright Data has plans on`},
		{BestPlansName, `{"slug":"bright-data"}`, `"error":`},
	} {
		msg, ok := tools.Call(ctx, openai.ToolCall{ID: "live", Function: openai.FunctionCall{Name: call.name, Arguments: call.arguments}})
		if !ok || !strings.Contains(msg.Content, call.has) {
			t.Errorf("%s(%s) = %s; want it to hold %s", call.name, call.arguments, msg.Content, call.has)
		}
		t.Logf("%s(%s) = %.300s", call.name, call.arguments, msg.Content)
	}
}
