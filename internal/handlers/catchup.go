package handlers

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/jiotv-go/jiotv_go/v3/internal/utils"
	"github.com/jiotv-go/jiotv_go/v3/pkg/epg"
	"github.com/jiotv-go/jiotv_go/v3/pkg/secureurl"
	"github.com/jiotv-go/jiotv_go/v3/pkg/television"
	pkgUtils "github.com/jiotv-go/jiotv_go/v3/pkg/utils"
)

// catchupParams holds the parsed catchup request parameters
type catchupParams struct {
	channelID   string
	channelIDInt int
	startTimeMs int64
	endTime     int64
	srno        string
	showID      string
}

// parseCatchupParams extracts and validates catchup parameters from the request
// Returns catchupParams or an error if validation fails
func parseCatchupParams(c *fiber.Ctx, id string) (*catchupParams, error) {
	// Remove suffix .m3u8 if exists
	id = strings.Replace(id, ".m3u8", "", 1)

	// Get start parameter (required)
	startStr := c.Query("start")
	if startStr == "" {
		return nil, fmt.Errorf("start parameter is required")
	}

	// Parse start time
	startTime, err := strconv.ParseInt(startStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid start time format")
	}

	// Convert start time from seconds to milliseconds
	startTimeMs := startTime * 1000

	// Parse channel ID to int for EPG lookup
	channelIDInt, err := strconv.Atoi(id)
	if err != nil {
		return nil, fmt.Errorf("invalid channel ID")
	}

	// Try to get EPG metadata from query params first (more reliable)
	srno := c.Query("srno")
	showID := c.Query("showId")

	var endTime int64

	// If srno and showId are provided, we can skip EPG lookup
	if srno != "" && showID != "" {
		endStr := c.Query("end")
		if endStr != "" {
			endTime, err = strconv.ParseInt(endStr, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid end time format")
			}
			endTime = endTime * 1000 // Convert to milliseconds
		} else {
			// If end time not provided, find it from EPG
			show, err := epg.FindShowByTime(channelIDInt, startTimeMs)
			if err != nil {
				pkgUtils.Log.Printf("Error finding show in EPG: %v", err)
				return nil, fmt.Errorf("no catchup content found for the specified time: %v", err)
			}
			endTime = show.EndEpoch
		}
	} else {
		// Find show in EPG to get metadata
		show, err := epg.FindShowByTime(channelIDInt, startTimeMs)
		if err != nil {
			pkgUtils.Log.Printf("Error finding show in EPG: %v", err)
			return nil, fmt.Errorf("no catchup content found for the specified time: %v", err)
		}
		endTime = show.EndEpoch
		srno = fmt.Sprintf("%d", show.Srno)
		showID = show.ShowID
	}

	return &catchupParams{
		channelID:   id,
		channelIDInt: channelIDInt,
		startTimeMs: startTimeMs,
		endTime:     endTime,
		srno:        srno,
		showID:      showID,
	}, nil
}

// getCatchupStream fetches the catchup stream from JioTV API
// Returns the catchup result or an error
func getCatchupStream(params *catchupParams) (*television.LiveURLOutput, error) {
	// For regular JioTV channels, ensure tokens are fresh before making API call
	if err := EnsureFreshTokens(); err != nil {
		pkgUtils.Log.Printf("Failed to ensure fresh tokens: %v", err)
		// Continue with the request - tokens might still work
	}

	// Call TV.Catchup to get the stream URL
	catchupResult, err := TV.Catchup(params.channelID, params.startTimeMs, params.endTime, params.srno, params.showID, params.startTimeMs)
	if err != nil {
		return nil, err
	}

	// Check if catchupResult.Bitrates.Auto is empty
	if catchupResult.Bitrates.Auto == "" {
		pkgUtils.Log.Printf("Catchup request - Channel: %s, Start: %d, End: %d, Srno: %s, ShowId: %s", 
			params.channelID, params.startTimeMs, params.endTime, params.srno, params.showID)
		pkgUtils.Log.Printf("Catchup result: %+v", catchupResult)
		return nil, fmt.Errorf("no catchup stream found for channel id: %s Status: %s", params.channelID, catchupResult.Message)
	}

	return catchupResult, nil
}

// buildCatchupRedirectURL constructs the redirect URL for catchup playback
func buildCatchupRedirectURL(catchupURL, channelID, quality, hdnea string) (string, error) {
	// Ensure hdnea from Catchup is appended to the URL
	if hdnea != "" && !strings.Contains(catchupURL, "hdnea=") {
		sep := "?"
		if strings.Contains(catchupURL, "?") {
			sep = "&"
		}
		catchupURL = catchupURL + sep + "hdnea=" + hdnea
	}

	// Encrypt the URL
	codedURL, err := secureurl.EncryptURL(catchupURL)
	if err != nil {
		return "", err
	}

	// Build redirect URL
	redirectURL := "/render.m3u8?auth=" + codedURL + "&channel_key_id=" + channelID
	if quality != "" {
		redirectURL += "&q=" + quality
	}
	if hdnea != "" {
		redirectURL += "&hdnea=" + hdnea
	}

	return redirectURL, nil
}

// CatchupHandler handles catchup stream route `/catchup/:id`
// Query parameters:
//   - start: Unix timestamp in seconds (required)
//   - end: Unix timestamp in seconds (optional, will be fetched from EPG if not provided)
//   - srno: Serial number from EPG (optional, improves reliability)
//   - showId: Show ID from EPG (optional, improves reliability)
func CatchupHandler(c *fiber.Ctx) error {
	id := c.Params("id")

	// Parse and validate parameters
	params, err := parseCatchupParams(c, id)
	if err != nil {
		return utils.BadRequestError(c, err.Error())
	}

	// Get catchup stream
	catchupResult, err := getCatchupStream(params)
	if err != nil {
		pkgUtils.Log.Println(err)
		if strings.Contains(err.Error(), "no catchup stream found") {
			return utils.NotFoundError(c, err.Error())
		}
		return utils.InternalServerError(c, err)
	}

	// Build redirect URL with auto quality
	redirectURL, err := buildCatchupRedirectURL(catchupResult.Bitrates.Auto, params.channelID, "", catchupResult.Hdnea)
	if err != nil {
		pkgUtils.Log.Println(err)
		return utils.ForbiddenError(c, err)
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

	// Parse and validate parameters
	params, err := parseCatchupParams(c, id)
	if err != nil {
		return utils.BadRequestError(c, err.Error())
	}

	// Get catchup stream
	catchupResult, err := getCatchupStream(params)
	if err != nil {
		pkgUtils.Log.Println(err)
		if strings.Contains(err.Error(), "no catchup stream found") {
			return utils.NotFoundError(c, err.Error())
		}
		return utils.InternalServerError(c, err)
	}

	// Select quality level based on query parameter
	bitrates := catchupResult.Bitrates
	catchupURL := utils.SelectQuality(quality, bitrates.Auto, bitrates.High, bitrates.Medium, bitrates.Low)

	// Build redirect URL with specified quality
	redirectURL, err := buildCatchupRedirectURL(catchupURL, params.channelID, quality, catchupResult.Hdnea)
	if err != nil {
		pkgUtils.Log.Println(err)
		return utils.ForbiddenError(c, err)
	}

	return c.Redirect(redirectURL, fiber.StatusFound)
}
