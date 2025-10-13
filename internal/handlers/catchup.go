package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jiotv-go/jiotv_go/v3/pkg/epg"
	"github.com/jiotv-go/jiotv_go/v3/pkg/secureurl"
	internalUtils "github.com/jiotv-go/jiotv_go/v3/internal/utils"
	"github.com/jiotv-go/jiotv_go/v3/pkg/utils"
)

// CatchupHandler handles catchup stream route `/catchup/:id`
// Query parameters:
//   - start: Unix timestamp in seconds (required)
//   - end: Unix timestamp in seconds (optional, will be fetched from EPG if not provided)
//   - srno: Serial number from EPG (optional, improves reliability)
//   - showId: Show ID from EPG (optional, improves reliability)
func CatchupHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	// remove suffix .m3u8 if exists
	id = strings.Replace(id, ".m3u8", "", 1)

	// Get parameters from query
	startStr := c.Query("start")
	if startStr == "" {
		return internalUtils.BadRequestError(c, "start parameter is required")
	}

	// Parse start time
	startTime, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		return internalUtils.BadRequestError(c, "invalid start time format")
	}

	// Convert start time from seconds to milliseconds
	startTimeMs := startTime * 1000

	// Parse channel ID to int for EPG lookup
	channelID, err := strconv.Atoi(id)
	if err != nil {
		return internalUtils.BadRequestError(c, "invalid channel ID")
	}

	// For regular JioTV channels, ensure tokens are fresh before making API call
	if err := EnsureFreshTokens(); err != nil {
		utils.Log.Printf("Failed to ensure fresh tokens: %v", err)
		// Continue with the request - tokens might still work
	}

	// Try to get EPG metadata from query params first (more reliable)
	srno := c.Query("srno")
	showId := c.Query("showId")

	var endTime int64
	var show *epg.EPGObject

	// If srno and showId are provided, we can skip EPG lookup
	if srno != "" && showId != "" {
		endStr := c.Query("end")
		if endStr != "" {
			endTime, err = strconv.ParseInt(endStr, 10, 64)
			if err != nil {
				return internalUtils.BadRequestError(c, "invalid end time format")
			}
			endTime = endTime * 1000 // Convert to milliseconds
		} else {
			// If end time not provided, find it from EPG
			show, err = epg.FindShowByTime(channelID, startTimeMs)
			if err != nil {
				utils.Log.Printf("Error finding show in EPG: %v", err)
				return internalUtils.NotFoundError(c, fmt.Sprintf("No catchup content found for the specified time: %v", err))
			}
			endTime = show.EndEpoch
		}
	} else {
		// Find show in EPG to get metadata
		show, err = epg.FindShowByTime(channelID, startTimeMs)
		if err != nil {
			utils.Log.Printf("Error finding show in EPG: %v", err)
			return internalUtils.NotFoundError(c, fmt.Sprintf("No catchup content found for the specified time: %v", err))
		}
		endTime = show.EndEpoch
		srno = fmt.Sprintf("%d", show.Srno)
		showId = show.ShowID
	}

	// Call TV.Catchup to get the stream URL
	catchupResult, err := TV.Catchup(id, startTimeMs, endTime, srno, showId, startTimeMs)
	if err != nil {
		utils.Log.Println(err)
		return internalUtils.InternalServerError(c, err)
	}

	// Check if catchupResult.Bitrates.Auto is empty
	if catchupResult.Bitrates.Auto == "" {
		error_message := "No catchup stream found for channel id: " + id + " Status: " + catchupResult.Message
		utils.Log.Println(error_message)
		utils.Log.Println(catchupResult)
		return internalUtils.NotFoundError(c, error_message)
	}

	// Ensure hdnea from Catchup is appended to subsequent requests
	catchupURL := catchupResult.Bitrates.Auto
	if catchupResult.Hdnea != "" && !strings.Contains(catchupURL, "hdnea=") {
		sep := "?"
		if strings.Contains(catchupURL, "?") {
			sep = "&"
		}
		catchupURL = catchupURL + sep + "hdnea=" + catchupResult.Hdnea
	}

	coded_url, err := secureurl.EncryptURL(catchupURL)
	if err != nil {
		utils.Log.Println(err)
		return internalUtils.ForbiddenError(c, err)
	}
	// also add hdnea as an explicit query param for downstream (no client cookie)
	redirectURL := "/render.m3u8?auth=" + coded_url + "&channel_key_id=" + id
	if catchupResult.Hdnea != "" {
		redirectURL += "&hdnea=" + catchupResult.Hdnea
	}
	return c.Redirect(redirectURL, fiber.StatusFound)
}

// CatchupQualityHandler handles catchup stream with quality route `/catchup/:quality/:id`
// Query parameters:
//   - start: Unix timestamp in seconds (required)
//   - end: Unix timestamp in seconds (optional, will be fetched from EPG if not provided)
//   - srno: Serial number from EPG (optional, improves reliability)
//   - showId: Show ID from EPG (optional, improves reliability)
func CatchupQualityHandler(c *fiber.Ctx) error {
	quality := c.Params("quality")
	id := c.Params("id")
	// remove suffix .m3u8 if exists
	id = strings.Replace(id, ".m3u8", "", 1)

	// Get parameters from query
	startStr := c.Query("start")
	if startStr == "" {
		return internalUtils.BadRequestError(c, "start parameter is required")
	}

	// Parse start time
	startTime, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		return internalUtils.BadRequestError(c, "invalid start time format")
	}

	// Convert start time from seconds to milliseconds
	startTimeMs := startTime * 1000

	// Parse channel ID to int for EPG lookup
	channelID, err := strconv.Atoi(id)
	if err != nil {
		return internalUtils.BadRequestError(c, "invalid channel ID")
	}

	// For regular JioTV channels, ensure tokens are fresh before making API call
	if err := EnsureFreshTokens(); err != nil {
		utils.Log.Printf("Failed to ensure fresh tokens: %v", err)
		// Continue with the request - tokens might still work
	}

	// Try to get EPG metadata from query params first (more reliable)
	srno := c.Query("srno")
	showId := c.Query("showId")

	var endTime int64
	var show *epg.EPGObject

	// If srno and showId are provided, we can skip EPG lookup
	if srno != "" && showId != "" {
		endStr := c.Query("end")
		if endStr != "" {
			endTime, err = strconv.ParseInt(endStr, 10, 64)
			if err != nil {
				return internalUtils.BadRequestError(c, "invalid end time format")
			}
			endTime = endTime * 1000 // Convert to milliseconds
		} else {
			// If end time not provided, find it from EPG
			show, err = epg.FindShowByTime(channelID, startTimeMs)
			if err != nil {
				utils.Log.Printf("Error finding show in EPG: %v", err)
				return internalUtils.NotFoundError(c, fmt.Sprintf("No catchup content found for the specified time: %v", err))
			}
			endTime = show.EndEpoch
		}
	} else {
		// Find show in EPG to get metadata
		show, err = epg.FindShowByTime(channelID, startTimeMs)
		if err != nil {
			utils.Log.Printf("Error finding show in EPG: %v", err)
			return internalUtils.NotFoundError(c, fmt.Sprintf("No catchup content found for the specified time: %v", err))
		}
		endTime = show.EndEpoch
		srno = fmt.Sprintf("%d", show.Srno)
		showId = show.ShowID
	}

	// Call TV.Catchup to get the stream URL
	catchupResult, err := TV.Catchup(id, startTimeMs, endTime, srno, showId, startTimeMs)
	if err != nil {
		utils.Log.Println(err)
		return internalUtils.InternalServerError(c, err)
	}

	Bitrates := catchupResult.Bitrates

	// select quality level based on query parameter
	catchupURL := internalUtils.SelectQuality(quality, Bitrates.Auto, Bitrates.High, Bitrates.Medium, Bitrates.Low)
	if catchupResult.Hdnea != "" && !strings.Contains(catchupURL, "hdnea=") {
		sep := "?"
		if strings.Contains(catchupURL, "?") {
			sep = "&"
		}
		catchupURL = catchupURL + sep + "hdnea=" + catchupResult.Hdnea
	}

	// quote url as it will be passed as a query parameter
	coded_url, err := secureurl.EncryptURL(catchupURL)
	if err != nil {
		utils.Log.Println(err)
		return internalUtils.ForbiddenError(c, err)
	}
	redirectURL := "/render.m3u8?auth=" + coded_url + "&channel_key_id=" + id + "&q=" + quality
	if catchupResult.Hdnea != "" {
		redirectURL += "&hdnea=" + catchupResult.Hdnea
	}
	return c.Redirect(redirectURL, fiber.StatusFound)
}
