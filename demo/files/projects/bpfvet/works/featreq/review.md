# featreq — review findings

| # | severity | location | finding |
|---|---|---|---|
| 1 | medium | internal/extract/requirements.go:88 | a tail-call target's features are not folded into the caller's set |
| 2 | medium | internal/extract/requirements.go:140 | map-in-map inner features are missed |
| 3 | low | internal/extract/requirements.go:31 | helper id reported without the program section |

Findings only, no patches. Waiting to discuss before anything is posted.
