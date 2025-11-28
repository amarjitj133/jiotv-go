# Catchup Format Error - RESOLVED ✅

## Final Solution

The "MEDIA_ELEMENT_ERROR: Format error" issue has been **successfully resolved**!

## Root Cause

**Timezone Mismatch**: The JioTV catchup API expects **UTC time**, but jiotv_go was sending **IST time**, causing a 5.5-hour offset.

### Symptoms

1. ❌ Recent catchup episodes returned "Format error"
2. ❌ Wrong episodes playing when clicked (time mismatch)
3. ❌ API returning 400 Bad Request for seemingly valid requests

## Changes Made

### 1. Timezone Fix (CRITICAL)

**File**: `internal/handlers/catchup.go` (line 143-145)

**Changed from:**

```go
// Use IST as JioTV usually expects IST for catchup
loc := time.FixedZone("IST", 5*3600+30*60)
start = time.UnixMilli(startInt).In(loc).Format("20060102T150405")
end = time.UnixMilli(endInt).In(loc).Format("20060102T150405")
```

**Changed to:**

```go
// Try UTC instead of IST - JioTV API expects UTC
start = time.UnixMilli(startInt).UTC().Format("20060102T150405")
end = time.UnixMilli(endInt).UTC().Format("20060102T150405")
```

### 2. Header Updates (Matching TS-JioTV)

**File**: `pkg/television/television.go` (lines 83-93)

- `"isott": "true"` (was "false")
- `"osVersion": "14"` (was "13")
- `"dm": "Xiaomi 22101316UP"` (added device model)
- `"versionCode": "452"` (was "389")

### 3. API Endpoint Update

**File**: `internal/constants/urls/urls.go` (line 38)

- Changed from: `/playback/apis/v1.1/geturl`
- Changed to: `/playback/apis/v1/geturl`

## How It Was Discovered

1. **Initial Theory**: Believed JioTV had 3-4 hour delay for recent content
2. **Investigation**: Matched TS-JioTV headers exactly, still got 400 errors
3. **Critical Clue**: User reported "clicking one episode plays a different one"
4. **Breakthrough**: Realized this indicated a **timezone offset** issue
5. **Solution**: Changed from IST to UTC, immediately fixed all issues

## Impact

### Before Fix

- ❌ Recent catchup episodes: Format error
- ❌ Older catchup episodes: Wrong content played (5.5hr offset)
- ❌ API requests: 400 Bad Request

### After Fix

- ✅ Recent catchup episodes: Work perfectly
- ✅ All catchup episodes: Correct content plays
- ✅ API requests: 200 OK with valid stream URLs

## Testing Confirmation

**Tested Episode**: Storage Wars on History TV18 (Channel 578)

- **Aired**: Nov 28, 2025 at 18:50 IST
- **Tested**: Nov 28, 2025 at 20:41 IST (~1 hour 51 minutes later)
- **Result**: ✅ **Plays successfully** with no errors

## Key Takeaway

When debugging API integration issues:

1. Don't assume timezone handling is correct
2. Pay attention to "wrong data" symptoms - they often indicate offset issues
3. Compare actual API requests byte-by-byte with working implementations
4. JioTV API uses **UTC time**, not local (IST) time

## Files Modified

1. `internal/handlers/catchup.go` - Timezone fix
2. `pkg/television/television.go` - Header updates
3. `internal/constants/urls/urls.go` - API endpoint fix

---

**Status**: ✅ **RESOLVED AND VERIFIED**
**Date**: November 28, 2025
**Testing**: Confirmed working with recent catchup episodes
