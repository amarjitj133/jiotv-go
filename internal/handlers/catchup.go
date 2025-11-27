package handlers

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	internalUtils "github.com/jiotv-go/jiotv_go/v3/internal/utils"
	"github.com/jiotv-go/jiotv_go/v3/pkg/secureurl"
	pkgUtils "github.com/jiotv-go/jiotv_go/v3/pkg/utils"
	"github.com/valyala/fasthttp"
)

const (
	catchupEPGURL   = "https://jiotvapi.cdn.jio.com/apis/v1.3/getepg/get?offset=%d&channel_id=%s&langId=%d"
	okhttpUserAgent = "okhttp/4.12.13"
	defaultLangID   = 6
)

// CatchupHandler renders the catchup UI for a specific channel
func CatchupHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	offsetStr := c.Query("offset", "0")
	offset, err := strconv.Atoi(offsetStr)
	if err != nil {
		offset = 0
		pkgUtils.Log.Printf("Invalid offset query parameter, defaulting to 0: %v", err)
	}

	// Fetch EPG data
	epgData, err := getCatchupEPG(id, offset)
	if err != nil {
		pkgUtils.Log.Println("Error fetching catchup EPG:", err)
		return c.Render("views/catchup", fiber.Map{
			"Title":   Title,
			"Error":   "Could not fetch catchup data",
			"Channel": id,
		})
	}

	// Filter out future programs
	currentTime := time.Now().UnixMilli()

	// Load IST location
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		loc = time.FixedZone("IST", 5*3600+30*60)
	}

	var pastEpgData []map[string]interface{}
	for _, p := range epgData {
		if start, ok := p["startEpoch"].(int64); ok {
			// Normalize to milliseconds if it looks like seconds
			if start < 100000000000 {
				start = start * 1000
			}

			if start > currentTime {
				continue
			}

			// Format times to IST AM/PM
			startTime := time.UnixMilli(start).In(loc)
			p["showtime"] = startTime.Format("03:04 PM")

			if end, ok := p["endEpoch"].(int64); ok {
				if end < 100000000000 {
					end = end * 1000
				}

				endTime := time.UnixMilli(end).In(loc)
				p["endtime"] = endTime.Format("03:04 PM")

				if start <= currentTime && end > currentTime {
					p["IsLive"] = true
				}
			}
		}
		pastEpgData = append(pastEpgData, p)
	}

	// Calculate Current Date for label
	// Offset is negative for past days. 0 = Today, -1 = Yesterday
	currentDate := time.Now().In(loc).AddDate(0, 0, offset).Format("02/01/2006")

	// Navigation limits
	// We allow going back up to 7 days (offset -7).
	// We do not allow going to future (offset > 0).
	showNext := offset < 0
	showPrev := offset > -7

	return c.Render("views/catchup", fiber.Map{
		"Title":       Title,
		"Data":        pastEpgData,
		"Channel":     id,
		"Offset":      offset,
		"NextOffset":  offset + 1,
		"PrevOffset":  offset - 1,
		"CurrentDate": currentDate,
		"ShowNext":    showNext,
		"ShowPrev":    showPrev,
	})
}

// CatchupStreamHandler handles the redirection to the catchup stream
func CatchupStreamHandler(c *fiber.Ctx) error {
	id := c.Params("id") // Channel ID
	// Query params: start (epoch), end (epoch)
	start := c.Query("start")
	end := c.Query("end")

	if start == "" || end == "" {
		return fiber.NewError(fiber.StatusBadRequest, "Missing start or end time")
	}

	// We need to construct the catchup URL using the JioTV API to get the signed URL and token.
	// This mirrors the logic in TS-JioTV's cpapi.php

	// We need to ensure we have fresh tokens/cookies.
	if err := EnsureFreshTokens(); err != nil {
		pkgUtils.Log.Printf("Failed to ensure fresh tokens: %v", err)
	}

	srno := c.Query("srno")
	if srno == "" {
		// Fallback if srno is missing (though it should be passed from UI)
		// Some channels might work without it or use a default, but best to have it.
		// For now, we proceed, GetCatchupURL might fail or API might reject.
		pkgUtils.Log.Println("Warning: srno is missing for catchup request")
	}

	// Fetch signed catchup URL and token
	catchupResult, err := TV.GetCatchupURL(id, srno, start, end)
	if err != nil {
		pkgUtils.Log.Printf("Error fetching catchup URL: %v", err)
		return internalUtils.InternalServerError(c, err)
	}

	targetURL := catchupResult.Result
	if targetURL == "" {
		targetURL = catchupResult.Bitrates.Auto
	}
	if targetURL == "" {
		return internalUtils.InternalServerError(c, fmt.Errorf("failed to get catchup URL from API"))
	}

	codedUrl, err := secureurl.EncryptURL(targetURL)
	if err != nil {
		return internalUtils.InternalServerError(c, err)
	}

	redirectURL := fmt.Sprintf("/render.m3u8?auth=%s&channel_key_id=%s", codedUrl, id)
	if catchupResult.Hdnea != "" {
		redirectURL += "&hdnea=" + catchupResult.Hdnea
	}
	return c.Redirect(redirectURL, fiber.StatusFound)
}

// CatchupPlayerHandler renders the catchup player UI
func CatchupPlayerHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	start := c.Query("start")
	end := c.Query("end")
	srno := c.Query("srno")
	showName := c.Query("showname", "Catchup Show")
	description := c.Query("description", "No description available")
	episodePoster := c.Query("poster", "")
	showTime := c.Query("showtime", "")

	playerURL := fmt.Sprintf("/catchup/render/%s?start=%s&end=%s&srno=%s", id, start, end, srno)

	return c.Render("views/catchup_player", fiber.Map{
		"Title":         Title,
		"ChannelID":     id,
		"ShowName":      showName,
		"Description":   description,
		"EpisodePoster": episodePoster,
		"ShowTime":      showTime,
		"player_url":    playerURL,
	})
}

// CatchupRenderPlayerHandler renders the HLS player inside the iframe
func CatchupRenderPlayerHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	start := c.Query("start")
	end := c.Query("end")
	srno := c.Query("srno")

	playURL := fmt.Sprintf("/catchup/stream/%s?start=%s&end=%s&srno=%s", id, start, end, srno)

	return c.Render("views/player_hls", fiber.Map{
		"play_url":   playURL,
		"is_catchup": true,
	})
}

// Helper to fetch EPG
func getCatchupEPG(id string, offset int) ([]map[string]interface{}, error) {
	url := fmt.Sprintf(catchupEPGURL, offset, id, defaultLangID)

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	req.SetRequestURI(url)
	req.Header.SetMethod("GET")
	req.Header.Set("Host", "jiotvapi.cdn.jio.com")
	req.Header.Set("user-agent", okhttpUserAgent)
	req.Header.Set("Accept-Encoding", "gzip")

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	client := &fasthttp.Client{}
	if err := client.Do(req, resp); err != nil {
		return nil, err
	}

	var body []byte
	var err error

	contentEncoding := resp.Header.Peek("Content-Encoding")
	if string(contentEncoding) == "gzip" {
		body, err = resp.BodyGunzip()
		if err != nil {
			return nil, err
		}
	} else {
		body = resp.Body()
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if epg, ok := result["epg"].([]interface{}); ok {
		// Convert []interface{} to []map[string]interface{}
		epgList := make([]map[string]interface{}, len(epg))
		for i, v := range epg {
			if m, ok := v.(map[string]interface{}); ok {
				// Ensure startEpoch and endEpoch are int64 to avoid scientific notation in templates
				if start, ok := m["startEpoch"].(float64); ok {
					m["startEpoch"] = int64(start)
				}
				if end, ok := m["endEpoch"].(float64); ok {
					m["endEpoch"] = int64(end)
				}
				epgList[i] = m
			}
		}
		return epgList, nil
	}

	return nil, fmt.Errorf("epg field not found or not a list")
}
