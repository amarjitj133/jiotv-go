---
description: Fix for missing quality options in catchup episodes
---

The issue was that the `CatchupStreamHandler` in `internal/handlers/catchup.go` was prioritizing the single stream result (`Result`) over the master playlist (`Bitrates.Auto`). Additionally, the `GetCatchupURL` function in `pkg/television/television.go` was using `stream_type: "Catchup"`, which might not return the full master playlist with bitrates.

The fix involves:
1. Prioritizing `Bitrates.Auto` in `CatchupStreamHandler` (done in previous steps).
2. Changing `stream_type` to `"Seek"` in `GetCatchupURL` to ensure the API returns the master playlist with all available qualities.

Steps taken:
1. Modified `internal/handlers/catchup.go`:
   - Changed priority to check `Bitrates.Auto` first, then fallback to `Result`.
2. Modified `pkg/television/television.go`:
   - Changed `formData.Add("stream_type", "Catchup")` to `formData.Add("stream_type", "Seek")`.

This ensures that the catchup URL returned by the JioTV API includes the master playlist with all available qualities, allowing Flowplayer to display the quality selection option.
