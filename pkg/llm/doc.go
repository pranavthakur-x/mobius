// Package llm implements unified client adapters, streaming SSE transport,
// configuration management, and credential storage for Large Language Model providers.
//
// # Provider Abstraction
//
// All model backends implement [Provider] (non-streaming [Provider.Generate]).
// Backends that support incremental token and tool-call delivery also implement
// [StreamingProvider] ([StreamingProvider.GenerateStream]). Callers can invoke
// [GenerateWithStream] to stream whenever supported and fall back to
// [Provider.Generate] otherwise.
//
// # Wire Protocols and Normalization
//
// Three native wire protocols are supported and registered via
// [RegisterProviderFactory]:
//   - OpenAI-compatible Chat Completions ("openai"), used by OpenAI, OpenRouter,
//     DeepSeek, vLLM, Ollama, and compatible endpoints.
//   - Anthropic Messages ("anthropic"), targeting /v1/messages.
//   - Google Gemini ("gemini"), targeting generateContent and streamGenerateContent.
//
// Before serialization, [NormalizeMessages] adjusts chat histories for provider
// wire constraints (such as non-empty tool results and default tool-call types and
// JSON arguments).
//
// # Streaming and HTTP Transport
//
// Streaming adapters parse Server-Sent Events using an unbounded line reader
// rather than a fixed-size scanner buffer, honoring [context.Context]
// cancellation and deadlines. HTTP clients created by the provider constructors
// configure hardened dial, TLS handshake, idle-connection, and HTTP/2 transport
// settings without setting a global [http.Client.Timeout] so long-lived SSE
// streams are never terminated prematurely. Error response bodies are bounded
// to 64 KiB.
//
// # Concurrency Guarantees
//
// [Config] guards its active model, configured providers, saved connections,
// and manual endpoint overrides with an internal [sync.RWMutex] and is safe for
// concurrent use by multiple goroutines. Provider factory registration via
// [RegisterProviderFactory] is intended to occur during package initialization
// before concurrent [Config.GetProviderForModel] or [Config.OpenGroupModel] calls.
package llm
