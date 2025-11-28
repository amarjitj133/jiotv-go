# JioTV Catchup Format Error - Root Cause Analysis

## Issue Summary

Users encounter "MEDIA_ELEMENT_ERROR: Format error" when attempting to play recent catchup episodes (episodes that aired within the last ~3 hours).

## Root Cause

The JioTV catchup API (`/playback/apis/v1.1/geturl`) returns **HTTP 400 Bad Request** for catchup content that is too recent. Through testing, we've identified:

### Test Results

- ❌ **1.5 hours ago**: API returns 400 Bad Request
- ❌ **2.0 hours ago**: API returns 400 Bad Request
- ❌ **2.5 hours ago**: API returns 400 Bad Request
- ❌ **3.0 hours ago**: API returns 400 Bad Request (needs verification)
- ✅ **12 hours ago**: API returns 200 OK with valid catchup URL

### API Response for Recent Episodes

```json
{
  "code": 400,
  "message": "Bad Request"
}
```

### API Request Format

```
POST /playback/apis/v1.1/geturl?langId=6
Content-Type: application/x-www-form-urlencoded

stream_type=Catchup
channel_id=578
programId=251128578040
showtime=000000
srno=251128578040
begin=20251128T185000
end=20251128T191500
```

## Current Flow (Problematic)

1. User clicks on a recent catchup episode (e.g., aired 1.5 hours ago)
2. `CatchupStreamHandler` calls `TV.GetCatchupURL()` with episode parameters
3. JioTV API returns **400 Bad Request**
4. `TV.GetCatchupURL()` returns an error
5. Handler tries to redirect anyway OR returns error to player
6. Player receives invalid/empty playlist
7. **Result**: "MEDIA_ELEMENT_ERROR: Format error"

## Why This Happens

JioTV enforces a **minimum delay** before catchup content becomes available (estimated 3-4 hours). This is likely due to:

- **Content ingestion pipeline delay**: Time needed to process and segment the broadcast
- **CDN synchronization**: Time to distribute catchup segments across CDN servers
- **Rights management**: Possible contractual restrictions on immediate replay

## Proposed Solutions

### Solution 1: Graceful Error Handling (Recommended - Immediate Fix)

Display a user-friendly message when catchup is not yet available:

**Changes needed in `CatchupStreamHandler` (catchup.go:110-176)**:

```go
func CatchupStreamHandler(c *fiber.Ctx) error {
    // ... existing code ...

    catchupResult, err := TV.GetCatchupURL(id, srno, start, end)
    if err != nil {
        pkgUtils.Log.Printf("Error fetching catchup URL: %v", err)

        // Check if it's a 400 error (content too recent)
        if strings.Contains(err.Error(), "status code: 400") {
            // Render error page with helpful message
            return c.Render("views/error", fiber.Map{
                "Title": Title,
                "ErrorTitle": "Catchup Not Yet Available",
                "ErrorMessage": "This episode is too recent. Catchup content typically becomes available 3-4 hours after broadcast. Please try again later.",
                "BackLink": fmt.Sprintf("/catchup/%s", id),
                "BackText": "Back to Catchup",
            })
        }

        return internalUtils.InternalServerError(c, err)
    }

    // ... rest of existing code ...
}
```

### Solution 2: Client-Side Prevention (Better UX)

Disable/hide episodes that are too recent in the catchup UI:

**Changes needed in `CatchupHandler` (catchup.go:25-107)**:

```go
const catchupDelayHours = 4 // Minimum hours before catchup becomes available

func CatchupHandler(c *fiber.Ctx) error {
    // ... existing code up to filtering ...

    currentTime := time.Now().UnixMilli()
    minAvailableTime := currentTime - (catchupDelayHours * 3600 * 1000)

    var pastEpgData []map[string]interface{}
    for _, p := range epgData {
        if start, ok := p["startEpoch"].(int64); ok {
            // Normalize to milliseconds
            if start < epochThreshold {
                start = start * 1000
            }

            if start > currentTime {
                continue // Future program
            }

            // Mark if too recent for catchup
            if start > minAvailableTime {
                p["TooRecentForCatchup"] = true
            }

            // ... rest of existing time formatting ...
        }
        pastEpgData = append(pastEpgData, p)
    }

    // ... rest of code ...
}
```

**Corresponding UI change in catchup.html**:

```html
{{range .Data}}
<div class="episode-card {{if .TooRecentForCatchup}}disabled{{end}}">
    {{if .TooRecentForCatchup}}
        <span class="badge">Available in {{calculateDelay .startEpoch}} hours</span>
        <a href="#" class="disabled-link" title="Catchup not yet available">
    {{else}}
        <a href="/catchup/play/{{$.Channel}}?start={{.startEpoch}}&end={{.endEpoch}}&srno={{.srno}}&showname={{.showname}}&description={{.description}}&poster={{.episodePoster}}&showtime={{.showtime}}">
    {{end}}
        <!-- episode content -->
    </a>
</div>
{{end}}
```

### Solution 3: Fallback to Live Stream (Most Seamless)

For very recent episodes (where end time > current time), use the live stream instead:

**Changes in `CatchupStreamHandler`**:

```go
func CatchupStreamHandler(c *fiber.Ctx) error {
    id := c.Params("id")
    start := c.Query("start")
    end := c.Query("end")

    // Parse end time
    endInt, _ := strconv.ParseInt(end, 10, 64)
    if endInt < epochThreshold {
        endInt = endInt * 1000
    }

    currentTime := time.Now().UnixMilli()

    // If episode ended very recently (within 4 hours), redirect to live stream
    if (currentTime - endInt) < (4 * 3600 * 1000) {
        pkgUtils.Log.Printf("Episode too recent for catchup, redirecting to live stream for channel %s", id)
        return c.Redirect(fmt.Sprintf("/live/%s.m3u8", id), fiber.StatusFound)
    }

    // ... existing catchup logic ...
}
```

## Recommended Implementation Order

1. **Immediate**: Implement Solution 1 (graceful error handling)
2. **Short-term**: Implement Solution 2 (UI prevention with visual indicators)
3. **Enhancement**: Consider Solution 3 for currently-airing programs

## Testing Checklist

- [ ] Test with episode from 1 hour ago (should fail gracefully)
- [ ] Test with episode from 3 hours ago (should fail gracefully)
- [ ] Test with episode from 6 hours ago (may work - verify)
- [ ] Test with episode from 12 hours ago (should work)
- [ ] Test with episode from yesterday (should work)
- [ ] Verify UI shows appropriate messages for unavailable content
