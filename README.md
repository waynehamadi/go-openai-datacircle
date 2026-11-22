# Datacircle tools for go-openai

Find the best GTM APIs. We benchmark all the GTM APIs and we put the results on our website: https://datacircle.dev

This Go module gives [go-openai](https://github.com/sashabaranov/go-openai) four function tools on Datacircle's open API, https://api.datacircle.dev/v1. You need no key and no account.

| Tool | What the tool gives | Call |
| --- | --- | --- |
| `datacircle_list_apis` | Every GTM API on Datacircle, with the API's slug, number of plans, number of providers and best plan | `GET /apis` |
| `datacircle_best_plans` | One API's plans, sorted by score out of 100, best first. Each plan comes with a rank and a price per 1,000 calls | `GET /apis/{slug}/best` |
| `datacircle_search_plans` | Plans that match a provider or plan name, on all APIs or on one API. Sort by score, provider or price | `GET /plans` |
| `datacircle_provider` | The APIs a provider sells, and the provider's rank on each API | `GET /providers/{slug}` |

## Install

```bash
go get github.com/waynehamadi/go-openai-datacircle
```

It needs go-openai v1.20.0 or later, and v1.42.0 or later for the Responses API.

## Chat completions

`Tools` gives the four tools for a request's `Tools`. `Call` runs a tool call of the model's message into the tool message that answers it.

```go
import datacircle "github.com/waynehamadi/go-openai-datacircle"

client := openai.NewClient(os.Getenv("OPENAI_API_KEY"))
tools := datacircle.New(nil)
messages := []openai.ChatCompletionMessage{
	{Role: openai.ChatMessageRoleUser, Content: "Find the 3 best providers for the email-finder API."},
}
for {
	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    "gpt-5",
		Messages: messages,
		Tools:    tools.Tools(),
	})
	if err != nil {
		log.Fatal(err)
	}
	msg := resp.Choices[0].Message
	messages = append(messages, msg)
	if len(msg.ToolCalls) == 0 {
		fmt.Println(msg.Content)
		break
	}
	for _, call := range msg.ToolCalls {
		answer, _ := tools.Call(ctx, call)
		messages = append(messages, answer)
	}
}
```

Your own tools can go in the same request: `append(tools.Tools(), yours...)`. `Call` gives `false` for a call of a tool that isn't one of the four, so you run it yourself. `Handles(name)` says the same before a call. A streamed reply works the same way: put each tool call together from its pieces, then give it to `Call`.

## Responses API

`Functions` gives the four tools' function definitions, and `openai.NewResponseFunctionTool` makes each one a tool. `Outputs` runs the function calls of a response and gives a `function_call_output` item for each one.

```go
var defs []openai.ResponseTool
for _, f := range tools.Functions() {
	defs = append(defs, openai.NewResponseFunctionTool(f))
}
input := []any{openai.ResponseInputMessage{Role: "user", Content: "Find the 3 best providers for the email-finder API."}}
for {
	resp, err := client.CreateResponse(ctx, openai.CreateResponseRequest{Model: "gpt-5", Input: input, Tools: defs})
	if err != nil {
		log.Fatal(err)
	}
	outputs := tools.Outputs(ctx, resp.Output)
	if len(outputs) == 0 {
		fmt.Println(resp.GetOutputText())
		break
	}
	input = append(append(input, resp.Output...), outputs...)
}
```

OpenAI also takes only the outputs with `PreviousResponseID: resp.ID`. `Outputs` gives nothing for a call of another function, so you run those yourself.

## Call a tool yourself

```go
out, err := datacircle.New(nil).Run(ctx, datacircle.BestPlansName, `{"slug":"email-finder","limit":3}`)
```

Each tool gives back a JSON object. Each plan comes with its rank, provider, plan, score, price per 1,000 (`perThousand`, in dollars) and its page on datacircle.dev. That page lists every field. A tool also takes an API's or a provider's name where it asks for a slug.

When the API gives no data, like for a slug it doesn't know, `Call` and `Outputs` give the model `{"error": "..."}` with the API's own words and what to send instead, so the model can call again. A network failure is given the same way. `Run` gives both as its error.

`datacircle.Config` sets the `HTTPClient` the tools use (a timeout, a proxy). `nil` uses `http.DefaultClient`.

## Network

Each tool call makes one HTTPS GET request to https://api.datacircle.dev/v1 and nothing else. The request holds only the tool's own arguments: no key, no config, nothing from your chats. The API takes up to 60 calls a minute from one IP address. The tool waits once, for a minute at most, when the API asks the tool to wait. The module depends only on go-openai and `golang.org/x/text`, and stores nothing.

## Links

- Datacircle: https://datacircle.dev
- The API: https://datacircle.dev/api
- The API's spec: https://api.datacircle.dev/v1/openapi.json
- What each key means: https://datacircle.dev/fields

MIT license.
