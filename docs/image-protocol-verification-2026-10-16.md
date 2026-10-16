# Image protocol verification, 2026-10-16

The test provider's model catalog identifies the requested GPT Image 2 model as
`gpt-image-2`. Real requests used the user-supplied `https://momoapi.xin` endpoint.
The API key was held in process memory and passed to the opt-in Go test through
its environment. No credentials or generated image payloads are committed.

## Real provider results

All of the following returned HTTP 200 with one decodable image:

| Request | Parameters |
| --- | --- |
| Text generation | 1024x1024, low, n=1 |
| JSON reference edit | 1024x1024, low, one PNG data URL |
| Multipart reference edit | 1024x1024, low, image[] upload |
| Automatic quality edit | 1024x1024, auto |
| Multiple-reference edit | 1536x1024, medium, two reference images |
| High quality edit | 1024x1536, high |
| Minimum landscape edit | 1088x608, low |
| 2K edit | 2048x2048, low |
| 4K edit | 3840x2160, low |
| OpenAI gateway edit | Actual ParseOpenAIImagesRequest/ForwardImages path, multipart upstream |
| Custom gateway edit | Actual ParseOpenAIImagesRequest/ForwardImages path, multipart upstream |

The direct JSON edit also worked. This does not establish a universal provider
JSON failure or reproduce the production site's reported failure. The changes
address confirmed compatibility gaps: invalid workbench dimensions, model
mapping restrictions, input aliases, upload encoding and lost multipart options.
Production account configuration and storage were not available in this session.

## Reproducible checks

The opt-in `TestImageProtocolLive` requires `IMAGE_TEST_BASE_URL`,
`IMAGE_TEST_API_KEY`, `IMAGE_TEST_REFERENCE` (a PNG path), and optionally
`IMAGE_TEST_MODEL`. Run `go test -tags=live ./internal/service -run
^TestImageProtocolLive$ -v -count=1`. This test makes paid requests and writes
the resulting images next to the reference file.

Offline tests cover model aliases, mapped upstream models, single/multiple
references, masks, MIME validation, JSON passthrough, native multipart,
Grok conversion, workbench task quantities, quality levels and model-specific
size bounds. Other providers are covered by protocol regression tests; the
supplied provider credential cannot establish their live availability.

Frontend verification includes 115 focused tests, TypeScript checking, scoped
ESLint, and a production build. Playwright checks at 1440x1000 and 390x844
covered model selection, reference uploads, quality and size options, invalid
ratios, and three independently submitted tasks, with no browser errors or
horizontal overflow. Browser screenshots were inspected locally.

OpenAI OAuth Responses and Grok changes have conversion/regression coverage.
Gemini, Vertex and Antigravity retain their native generation protocols; the
OpenAI Images protocol change does not replace those provider APIs.
