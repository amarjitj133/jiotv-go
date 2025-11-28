package handlers

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jiotv-go/jiotv_go/v3/internal/config"
	"github.com/jiotv-go/jiotv_go/v3/internal/constants/headers"
	"github.com/jiotv-go/jiotv_go/v3/internal/constants/urls"
	"github.com/jiotv-go/jiotv_go/v3/internal/plugins"
	internalUtils "github.com/jiotv-go/jiotv_go/v3/internal/utils"
	"github.com/jiotv-go/jiotv_go/v3/pkg/secureurl"
	"github.com/jiotv-go/jiotv_go/v3/pkg/television"
	"github.com/jiotv-go/jiotv_go/v3/pkg/utils"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/proxy"
)

var (
	TV               *television.Television
	DisableTSHandler bool
	isLogoutDisabled bool
	Title            string
	EnableDRM        bool
	SONY_LIST        = []string{"154", "155", "162", "289", "291", "471", "474", "476", "483", "514", "524", "525", "697", "872", "873", "874", "891", "892", "1146", "1393", "1772", "1773", "1774", "1775"}
)

const (
	REFRESH_TOKEN_URL     = urls.RefreshTokenURL
	REFRESH_SSO_TOKEN_URL = urls.RefreshSSOTokenURL
	PLAYER_USER_AGENT     = headers.UserAgentPlayTV
	REQUEST_USER_AGENT    = headers.UserAgentOkHttp
	HDNEA_PARAM           = "hdnea="
	PLAYER_ROUTE_PREFIX   = "/player/"
	VIEWS_INDEX           = "views/index"
)

// Init initializes the necessary operations required for the handlers to work.
func Init() {
	if config.Cfg.Title != "" {
		Title = config.Cfg.Title
	} else {
		Title = "JioTV Go"
	}
	DisableTSHandler = config.Cfg.DisableTSHandler
	isLogoutDisabled = config.Cfg.DisableLogout
	EnableDRM = true // DRM is enabled by default, only channels that support DRM will use it
	if DisableTSHandler {
		utils.Log.Println("TS Handler disabled!. All TS video requests will be served directly from JioTV servers.")
	}
	if !EnableDRM {
		utils.Log.Println("If you're not using IPTV Client. We strongly recommend enabling DRM for accessing channels without any issues! Either enable by setting environment variable JIOTV_DRM=true or by setting DRM: true in config. For more info Read https://telegram.me/jiotv_go/128")
	}
	// Generate a new device ID if not present
	utils.GetDeviceID()
	// Get credentials from file
	credentials, err := utils.GetJIOTVCredentials()
	// Initialize TV object with nil credentials initially
	TV = television.New(nil)
	if err != nil {
		utils.Log.Println("Login error!", err)
	} else {
		// If AccessToken is present, validate on first use
		if credentials.AccessToken != "" && credentials.RefreshToken == "" {
			utils.Log.Println("Warning: AccessToken present but RefreshToken is missing. Token refresh may fail.")
		}
		// If SsoToken is present, validate on first use
		if credentials.SSOToken != "" && credentials.UniqueID == "" {
			utils.Log.Println("Warning: SSOToken present but UniqueID is missing. Token refresh may fail.")
		}
		// Initialize TV object with credentials
		TV = television.New(credentials)
	}

	// Initialize custom channels at startup if configured
	television.InitCustomChannels()
}

// ErrorMessageHandler handles error messages
// Responds with 500 status code and error message
func ErrorMessageHandler(c *fiber.Ctx, err error) error {
	if err != nil {
		return internalUtils.InternalServerError(c, err.Error())
	}
	return nil
}

// getPluginChannel checks if a channel ID belongs to a plugin
func getPluginChannel(id string) (*television.Channel, bool) {
	if len(config.Cfg.Plugins) > 0 {
		channels := plugins.GetChannels()
		for _, ch := range channels {
			if ch.ID == id {
				return &ch, true
			}
		}
	} else {
		utils.Log.Println("No plugins enabled or config.Cfg.Plugins is empty")
	}
	return nil, false
}

// isCustomChannel checks if a given channel ID is a custom channel
func isCustomChannel(channelID string) bool {
	if config.Cfg.CustomChannelsFile == "" {
		return false
	}

	// Check direct lookup with the provided ID
	if _, exists := television.GetCustomChannelByID(channelID); exists {
		return true
	}

	return false
}

// getChannelCatchupSupport checks if a channel supports catchup
func getChannelCatchupSupport(channelID string) bool {
	// Fetch all channels
	channelsResponse, err := television.Channels()
	if err != nil {
		utils.Log.Printf("Error fetching channels: %v", err)
		return false
	}

	// Find the channel by ID
	for _, channel := range channelsResponse.Result {
		if channel.ID == channelID {
			return channel.IsCatchupAvailable
		}
	}

	// Default to false if channel not found
	return false
}

// IndexHandler handles the index page for `/` route
func IndexHandler(c *fiber.Ctx) error {
	// Get all channels
	channels, err := television.Channels()
	if err != nil {
		return ErrorMessageHandler(c, err)
	}

	if len(config.Cfg.Plugins) > 0 {
		pluginChannels := plugins.GetChannels()
		channels.Result = append(channels.Result, pluginChannels...)
	}

	// Get language and category from query params
	language := c.Query("language")
	category := c.Query("category")

	// Process logo URLs for all channels
	hostURL := c.Protocol() + "://" + c.Get("Host")
	for i, channel := range channels.Result {
		if strings.HasPrefix(channel.LogoURL, "http://") || strings.HasPrefix(channel.LogoURL, "https://") {
			// Custom channel with full URL, use as-is
			channels.Result[i].LogoURL = channel.LogoURL
		} else {
			// Regular channel with relative path, add proxy prefix
			channels.Result[i].LogoURL = hostURL + "/jtvimage/" + channel.LogoURL
		}
	}

	// Context data for index page
	indexContext := fiber.Map{
		"Title":         Title,
		"Channels":      nil,
		"IsNotLoggedIn": !utils.CheckLoggedIn(),
		"Categories":    television.CategoryMap,
		"Languages":     television.LanguageMap,
		"Qualities": map[string]string{
			"auto":   "Quality (Auto)",
			"high":   "High",
			"medium": "Medium",
			"low":    "Low",
		},
	}

	// Load login config to get default mobile number
	loginConfig, err := config.LoadLoginConfig()
	if err == nil && loginConfig.MobileNumber != "" {
		indexContext["MobileNumber"] = loginConfig.MobileNumber
	}

	// Filter channels by query params if provided
	if language != "" || category != "" {
		language_int, err := strconv.Atoi(language)
		if err != nil {
			return ErrorMessageHandler(c, err)
		}
		category_int, err := strconv.Atoi(category)
		if err != nil {
			return ErrorMessageHandler(c, err)
		}
		channels_list := television.FilterChannels(channels.Result, language_int, category_int)
		indexContext["Channels"] = channels_list
		return c.Render(VIEWS_INDEX, indexContext)
	}

	// If no query parameters are provided, use default config filtering
	if len(config.Cfg.DefaultCategories) > 0 || len(config.Cfg.DefaultLanguages) > 0 {
		channels_list := television.FilterChannelsByDefaults(channels.Result, config.Cfg.DefaultCategories, config.Cfg.DefaultLanguages)
		indexContext["Channels"] = channels_list
		return c.Render(VIEWS_INDEX, indexContext)
	}

	// If no query params and no default config, return all channels
	indexContext["Channels"] = channels.Result
	return c.Render(VIEWS_INDEX, indexContext)
}

// checkFieldExist checks if the field is provided in the request.
// If not, send a bad request response
func checkFieldExist(field string, check bool, c *fiber.Ctx) error {
	return internalUtils.CheckFieldExist(c, field, check)
}

// LiveHandler handles the live channel stream route `/live/:id.m3u8`.
func LiveHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	// remove suffix .m3u8 if exists
	id = strings.Replace(id, ".m3u8", "", 1)

	// Check if this is a custom channel - serve directly for custom channels
	if isCustomChannel(id) {
		channel, exists := television.GetCustomChannelByID(id)
		if !exists {
			utils.Log.Printf("Custom channel with ID %s not found", id)
			return internalUtils.NotFoundError(c, fmt.Sprintf("Custom channel with ID %s not found", id))
		}
		// For custom channels, redirect directly to the m3u8 URL (no render pipeline needed)
		return c.Redirect(channel.URL, fiber.StatusFound)
	}

	if channel, exists := getPluginChannel(id); exists {
		return c.Redirect("/"+channel.URL, fiber.StatusFound)
	}

	// For regular JioTV channels, ensure tokens are fresh before making API call
	if err := EnsureFreshTokens(); err != nil {
		utils.Log.Printf("Failed to ensure fresh tokens: %v", err)
		// Continue with the request - tokens might still work
	}

	liveResult, err := TV.Live(id)
	if err != nil {
		utils.Log.Println(err)
		return internalUtils.InternalServerError(c, err)
	}

	// Check if liveResult.Bitrates.Auto is empty
	if liveResult.Bitrates.Auto == "" {
		error_message := "No stream found for channel id: " + id + "Status: " + liveResult.Message
		utils.Log.Println(error_message)
		utils.Log.Println(liveResult)
		return internalUtils.NotFoundError(c, error_message)
	}
	// quote url as it will be passed as a query parameter
	// It is required to quote the url as it may contain special characters like ? and &
	// Ensure hdnea from Live is appended to subsequent requests
	liveURL := liveResult.Bitrates.Auto
	if liveResult.Hdnea != "" && !strings.Contains(liveURL, HDNEA_PARAM) {
		sep := "?"
		if strings.Contains(liveURL, "?") {
			sep = "&"
		}
		liveURL = liveURL + sep + HDNEA_PARAM + liveResult.Hdnea
	}

	coded_url, err := secureurl.EncryptURL(liveURL)
	if err != nil {
		utils.Log.Println(err)
		return internalUtils.ForbiddenError(c, err)
	}
	// also add hdnea as an explicit query param for downstream (no client cookie)
	redirectURL := "/render.m3u8?auth=" + coded_url + "&channel_key_id=" + id
	if liveResult.Hdnea != "" {
		redirectURL += "&hdnea=" + liveResult.Hdnea
	}
	return c.Redirect(redirectURL, fiber.StatusFound)
}

// LiveQualityHandler handles the live channel stream route `/live/:quality/:id.m3u8`.
func LiveQualityHandler(c *fiber.Ctx) error {
	quality := c.Params("quality")
	id := c.Params("id")
	// remove suffix .m3u8 if exists
	id = strings.Replace(id, ".m3u8", "", 1)

	// Check if this is a custom channel - serve directly for custom channels
	if isCustomChannel(id) {
		channel, exists := television.GetCustomChannelByID(id)
		if !exists {
			utils.Log.Printf("Custom channel with ID %s not found", id)
			return internalUtils.NotFoundError(c, fmt.Sprintf("Custom channel with ID %s not found", id))
		}
		// For custom channels, redirect directly to the m3u8 URL (no render pipeline needed)
		return c.Redirect(channel.URL, fiber.StatusFound)
	}

	if channel, exists := getPluginChannel(id); exists {
		return c.Redirect("/"+channel.URL, fiber.StatusFound)
	}

	// For regular JioTV channels, ensure tokens are fresh before making API call
	if err := EnsureFreshTokens(); err != nil {
		utils.Log.Printf("Failed to ensure fresh tokens: %v", err)
		// Continue with the request - tokens might still work
	}

	liveResult, err := TV.Live(id)
	if err != nil {
		utils.Log.Println(err)
		return internalUtils.InternalServerError(c, err)
	}
	Bitrates := liveResult.Bitrates
	// if id[:2] == "sl" {
	// 	return sonyLivRedirect(c, liveResult)
	// }
	// Channels with following IDs output audio only m3u8 when quality level is enforced
	if id == "1349" || id == "1322" {
		quality = "auto"
	}

	// select quality level based on query parameter
	liveURL := internalUtils.SelectQuality(quality, Bitrates.Auto, Bitrates.High, Bitrates.Medium, Bitrates.Low)
	if liveResult.Hdnea != "" && !strings.Contains(liveURL, HDNEA_PARAM) {
		sep := "?"
		if strings.Contains(liveURL, "?") {
			sep = "&"
		}
		liveURL = liveURL + sep + HDNEA_PARAM + liveResult.Hdnea
	}

	// quote url as it will be passed as a query parameter
	coded_url, err := secureurl.EncryptURL(liveURL)
	if err != nil {
		utils.Log.Println(err)
		return internalUtils.ForbiddenError(c, err)
	}
	redirectURL := "/render.m3u8?auth=" + coded_url + "&channel_key_id=" + id + "&q=" + quality
	if liveResult.Hdnea != "" {
		redirectURL += "&hdnea=" + liveResult.Hdnea
	}
	return c.Redirect(redirectURL, fiber.StatusFound)
}

// RenderHandler handles M3U8 file for modification
// This handler shall replace JioTV server URLs with our own server URLs
func RenderHandler(c *fiber.Ctx) error {
	// URL to be rendered
	auth := c.Query("auth")
	fmt.Println("RenderHandler called with auth:", auth)
	if err := internalUtils.ValidateRequiredParam("auth", auth); err != nil {
		return err
	}
	// Channel ID to be used for key rendering
	channel_id := c.Query("channel_key_id")
	if err := internalUtils.ValidateRequiredParam("channel_key_id", channel_id); err != nil {
		return err
	}
	// decrypt url
	decoded_url, err := secureurl.DecryptURL(auth)
	if err != nil {
		utils.Log.Println(err)
		return err
	}
	fmt.Println("RenderHandler decoded URL:", decoded_url)

	// If hdnea is present in query and missing in the decrypted URL, append it so TV.Render can forward as request cookie upstream
	if hdnea := c.Query("hdnea"); hdnea != "" && !strings.Contains(decoded_url, HDNEA_PARAM) {
		sep := "?"
		if strings.Contains(decoded_url, "?") {
			sep = "&"
		}
		decoded_url = decoded_url + sep + HDNEA_PARAM + hdnea
	}

	renderResult, statusCode, newHdnea := TV.Render(decoded_url)
	utils.Log.Printf("Render: fetched %d bytes from %s, status: %d", len(renderResult), decoded_url, statusCode)
	if len(renderResult) == 0 {
		utils.Log.Println("Render: WARNING: Received empty body from TV.Render")
	} else if len(renderResult) < 200 {
		utils.Log.Printf("Render: Short body received: %s", string(renderResult))
	}

	// If we get a 403 (Forbidden), try refreshing tokens and retry once
	if statusCode == fiber.StatusForbidden {
		if err := EnsureFreshTokens(); err != nil {
			utils.Log.Printf("Failed to refresh tokens after 403: %v", err)
			// Retry the request once after refreshing tokens
			utils.Log.Println("Retrying render request after token refresh")
			renderResult, statusCode, newHdnea = TV.Render(decoded_url)
		} else {
			utils.Log.Println("Unable to refresh tokens after expiration")
			return internalUtils.ForbiddenError(c, "Access forbidden. Something went wrong!")
		}
	}
	// No client cookie: if upstream rotated __hdnea__, we'll embed the fresh token into rewritten URLs below

	// baseUrl is the part of the url excluding suffix file.m3u8 and params is the part of the url after the suffix
	split_url_by_params := strings.Split(decoded_url, "?")
	baseStringUrl := split_url_by_params[0]
	// Pattern to match file names ending with .m3u8
	pattern := `[a-z0-9=\_\-A-Z\.]*\.m3u8`
	re := regexp.MustCompile(pattern)
	// Add baseUrl to all the file names ending with .m3u8
	baseUrl := []byte(re.ReplaceAllString(baseStringUrl, ""))
	var params string
	if len(split_url_by_params) > 1 {
		params = split_url_by_params[1]
	}
	// If upstream rotated __hdnea__, update params so rewritten URLs carry the fresh token value
	if newHdnea != "" {
		if strings.Contains(params, HDNEA_PARAM) {
			parts := strings.Split(params, "&")
			for i, p := range parts {
				if strings.HasPrefix(p, HDNEA_PARAM) {
					parts[i] = HDNEA_PARAM + newHdnea
					break
				}
			}
			params = strings.Join(parts, "&")
		} else {
			if params == "" {
				params = HDNEA_PARAM + newHdnea
			} else {
				params = params + "&hdnea=" + newHdnea
			}
		}
	}

	// replacer replaces all the file names ending with .m3u8 and .ts with our own server URLs
	// More info: https://golang.org/pkg/regexp/#Regexp.ReplaceAllFunc
	replacer := func(match []byte) []byte {
		switch {
		case bytes.Contains(match, []byte(".m3u8")):
			return television.ReplaceM3U8(baseUrl, match, params, channel_id, c.Query("q"))
		case bytes.Contains(match, []byte(".ts")):
			return television.ReplaceTS(baseUrl, match, params)
		case bytes.Contains(match, []byte(".aac")):
			return television.ReplaceAAC(baseUrl, match, params)
		default:
			return match
		}
	}

	// Pattern to match file names ending with .m3u8 and .ts
	pattern = `[a-z0-9=\_\-A-Z\/\.]*\.(m3u8|ts|aac)(\?[^\s"]*)?`
	re = regexp.MustCompile(pattern)

	// For catchup streams with variants, reorder them to put highest quality first
	if bytes.Contains(renderResult, []byte("vbegin=")) && bytes.Contains(renderResult, []byte("#EXT-X-STREAM-INF")) {
		renderResult = reorderCatchupVariants(renderResult)
	}

	// Execute replacer function on renderResult
	renderResult = re.ReplaceAllFunc(renderResult, replacer)

	// replacer_key replaces all the URLs ending with .key and .pkey with our own server URLs
	replacer_key := func(match []byte) []byte {
		switch {
		case bytes.HasSuffix(match, []byte(".key")) || bytes.HasSuffix(match, []byte(".pkey")):
			return television.ReplaceKey(match, params, channel_id)
		default:
			return match
		}
	}

	// Pattern to match URLs ending with .key and .pkey
	pattern_key := `http[\S]+\.(pkey|key)`
	re_key := regexp.MustCompile(pattern_key)

	// Execute replacer_key function on renderResult
	renderResult = re_key.ReplaceAllFunc(renderResult, replacer_key)

	if statusCode != fiber.StatusOK {
		utils.Log.Println("Error rendering M3U8 file")
		utils.Log.Println(string(renderResult))
	}

	// Debug: save processed catchup playlist
	// Detect by checking if it has variants (master playlist) and channel_key_id in path
	// Debug: save processed catchup playlist
	// Detect by checking if it has variants (master playlist) and channel_key_id in path
	if bytes.Contains(renderResult, []byte("#EXT-X-STREAM-INF")) && channel_id != "" {
		if f, err := os.OpenFile("/tmp/catchup_processed.m3u8", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			f.Write(renderResult)
			f.Close()
			utils.Log.Println("Debug: Wrote processed catchup playlist to /tmp/catchup_processed.m3u8")
		} else {
			utils.Log.Printf("Debug: Failed to write catchup playlist: %v", err)
		}
	}
	internalUtils.SetMustRevalidateHeader(c, 3)
	c.Set("Content-Type", "application/vnd.apple.mpegurl")
	return c.Status(statusCode).Send(renderResult)
}

// SLHandler proxies requests to SonyLiv CDN
func SLHandler(c *fiber.Ctx) error {
	// Request path with query params
	url := "https://lin-gd-001-cf.slivcdn.com" + c.Path() + "?" + string(c.Request().URI().QueryString())
	if url[len(url)-1:] == "?" {
		url = url[:len(url)-1]
	}
	// Delete all browser headers
	internalUtils.SetPlayerHeaders(c, PLAYER_USER_AGENT)
	if err := proxy.Do(c, url, TV.Client); err != nil {
		return err
	}

	c.Response().Header.Del(fiber.HeaderServer)
	c.Response().Header.Add("Access-Control-Allow-Origin", "*")
	return nil
}

// RenderKeyHandler requests m3u8 key from JioTV server
func RenderKeyHandler(c *fiber.Ctx) error {
	channel_id := c.Query("channel_key_id")
	auth := c.Query("auth")
	// parse incoming hdnea query and set as request cookie only for upstream call (no client cookie)
	if hdnea := c.Query("hdnea"); hdnea != "" {
		c.Request().Header.SetCookie("__hdnea__", hdnea)
	}
	// decode url
	decoded_url, err := internalUtils.DecryptURLParam("auth", auth)
	if err != nil {
		return err
	}

	// extract params from url
	var params string
	if strings.Contains(decoded_url, "?") {
		params = strings.Split(decoded_url, "?")[1]
	}

	// set params as cookies as JioTV uses cookies to authenticate
	if params != "" {
		for _, param := range strings.Split(params, "&") {
			if strings.Contains(param, "=") {
				key := strings.Split(param, "=")[0]
				value := strings.Split(param, "=")[1]
				c.Request().Header.SetCookie(key, value)
			}
		}
		// ensure __hdnea__ cookie exists if available from params
		if strings.Contains(params, HDNEA_PARAM) {
			for _, p := range strings.Split(params, "&") {
				if strings.HasPrefix(p, HDNEA_PARAM) {
					c.Request().Header.SetCookie("__hdnea__", strings.TrimPrefix(p, HDNEA_PARAM))
					break
				}
			}
		}
	}

	// Copy headers from the Television headers map to the request
	for key, value := range TV.Headers {
		c.Request().Header.Set(key, value) // Assuming only one value for each header
	}
	c.Request().Header.Set("srno", "230203144000")
	c.Request().Header.Set("ssotoken", TV.SsoToken)
	c.Request().Header.Set("channelId", channel_id)
	c.Request().Header.Set("User-Agent", PLAYER_USER_AGENT)
	if err := proxy.Do(c, decoded_url, TV.Client); err != nil {
		return err
	}
	c.Response().Header.Del(fiber.HeaderServer)
	return nil
}

// RenderTSHandler loads TS file from JioTV server
func RenderTSHandler(c *fiber.Ctx) error {
	auth := c.Query("auth")
	// parse incoming hdnea query and set as request cookie only for upstream call (no client cookie)
	if hdnea := c.Query("hdnea"); hdnea != "" {
		c.Request().Header.SetCookie("__hdnea__", hdnea)
	}
	// decode url
	decoded_url, err := internalUtils.DecryptURLParam("auth", auth)
	if err != nil {
		utils.Log.Panicln(err)
		return err
	}
	return internalUtils.ProxyRequest(c, decoded_url, TV.Client, PLAYER_USER_AGENT)
}

// ChannelsHandler fetch all channels from JioTV API
// Also to generate M3U playlist
func ChannelsHandler(c *fiber.Ctx) error {

	quality := strings.TrimSpace(c.Query("q"))
	splitCategory := strings.TrimSpace(c.Query("c"))
	languages := strings.TrimSpace(c.Query("l"))
	skipGenres := strings.TrimSpace(c.Query("sg"))
	apiResponse, err := television.Channels()
	if err != nil {
		return ErrorMessageHandler(c, err)
	}

	if len(config.Cfg.Plugins) > 0 {
		pluginChannels := plugins.GetChannels()
		apiResponse.Result = append(apiResponse.Result, pluginChannels...)
	}
	// hostUrl should be request URL like http://localhost:5001
	hostURL := strings.ToLower(c.Protocol()) + "://" + c.Get("Host")

	// Check if the query parameter "type" is set to "m3u"
	if c.Query("type") == "m3u" {
		// Create an M3U playlist
		m3uContent := "#EXTM3U x-tvg-url=\"" + hostURL + "/epg.xml.gz\"\n"
		logoURL := hostURL + "/jtvimage"
		for _, channel := range apiResponse.Result {

			if languages != "" && !utils.ContainsString(television.LanguageMap[channel.Language], strings.Split(languages, ",")) {
				continue
			}

			if skipGenres != "" && utils.ContainsString(television.CategoryMap[channel.Category], strings.Split(skipGenres, ",")) {
				continue
			}

			var channelURL string
			if !channel.IsCustom {
				if quality != "" {
					channelURL = fmt.Sprintf("%s/live/%s/%s.m3u8", hostURL, quality, channel.ID)
				} else {
					channelURL = fmt.Sprintf("%s/live/%s.m3u8", hostURL, channel.ID)
				}
			} else {
				channelURL = fmt.Sprintf("%s/%s", hostURL, channel.URL)
			}
			var channelLogoURL string
			if strings.HasPrefix(channel.LogoURL, "http://") || strings.HasPrefix(channel.LogoURL, "https://") {
				// Custom channel with full URL
				channelLogoURL = channel.LogoURL
			} else {
				// Regular channel with relative path
				channelLogoURL = fmt.Sprintf("%s/%s", logoURL, channel.LogoURL)
			}
			var groupTitle string
			switch splitCategory {
			case "split":
				groupTitle = fmt.Sprintf("%s - %s", television.CategoryMap[channel.Category], television.LanguageMap[channel.Language])
			case "language":
				groupTitle = television.LanguageMap[channel.Language]
			default:
				groupTitle = television.CategoryMap[channel.Category]
			}

			// Build catchup attributes if channel supports catchup
			var catchupAttrs string
			if channel.IsCatchupAvailable {
				// Enhanced catchup source URL pattern for IPTV players (Tivimate/OTT Navigator compatible)
				// Uses standard catchup variables: ${catchup-id}, ${start}, ${stop} (or ${timestamp}, ${duration})
				// Format optimized for TiviMate and OTT Navigator
				catchupSource := fmt.Sprintf("%s/catchup/render/%s?start=${start}&end=${stop}", hostURL, channel.ID)
				// Use "append" mode for better compatibility, with 7 days catchup
				catchupAttrs = fmt.Sprintf(` catchup="append" catchup-days="7" catchup-source="%s"`, catchupSource)
			}
			m3uContent += fmt.Sprintf("#EXTINF:-1 tvg-id=%q tvg-name=%q tvg-logo=%q tvg-language=%q tvg-type=%q group-title=%q%s, %s\n%s\n",
				channel.ID, channel.Name, channelLogoURL, television.LanguageMap[channel.Language], television.CategoryMap[channel.Category], groupTitle, catchupAttrs, channel.Name, channelURL)
		}

		// Set the Content-Disposition header for file download
		c.Set("Content-Disposition", "attachment; filename=jiotv_playlist.m3u")
		c.Set("Content-Type", "application/vnd.apple.mpegurl") // Set the video M3U MIME type
		return c.SendStream(strings.NewReader(m3uContent))
	}

	for i, channel := range apiResponse.Result {
		if !channel.IsCustom {
			apiResponse.Result[i].URL = fmt.Sprintf("%s/live/%s", hostURL, channel.ID)
		}
	}

	return c.JSON(apiResponse)
}

// PlayHandler loads HTML Page with video player iframe embedded with video URL
// URL is generated from the channel ID
func PlayHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	quality := c.Query("q")

	// Ensure tokens are fresh before making API call for DRM channels
	if err := EnsureFreshTokens(); err != nil {
		utils.Log.Printf("Failed to ensure fresh tokens: %v", err)
		// Continue with the request - tokens might still work or it might be a custom channel
	}

	var player_url string
	if EnableDRM {
		// Some sonyLiv channels are DRM protected and others are not
		// In order to check, we need to make additional request to JioTV API
		// Quick dirty fix, otherwise we need to refactor entire LiveTV Handler approach
		if utils.ContainsString(id, SONY_LIST) {
			liveResult, err := TV.Live(id)
			if err != nil {
				utils.Log.Println(err)
				return internalUtils.InternalServerError(c, err)
			}
			// if drm is available, use DRM player
			if liveResult.IsDRM {
				player_url = "/mpd/" + id + "?q=" + quality
			} else {
				// if not, use HLS player
				player_url = PLAYER_ROUTE_PREFIX + id + "?q=" + quality
			}
		} else if isCustomChannel(id) {
			player_url = PLAYER_ROUTE_PREFIX + id + "?q=" + quality
		} else if _, exists := getPluginChannel(id); exists {
			player_url = PLAYER_ROUTE_PREFIX + id + "?q=" + quality
		} else {
			player_url = "/mpd/" + id + "?q=" + quality
		}
	} else {
		player_url = PLAYER_ROUTE_PREFIX + id + "?q=" + quality
	}
	internalUtils.SetCacheHeader(c, 3600)
	return c.Render("views/play", fiber.Map{
		"Title":              Title,
		"player_url":         player_url,
		"ChannelID":          id,
		"IsCatchupAvailable": getChannelCatchupSupport(id),
	})
}

// PlayerHandler loads Web Player to stream live TV
func PlayerHandler(c *fiber.Ctx) error {
	id := c.Params("id")
	quality := c.Query("q")
	play_url := utils.BuildHLSPlayURL(quality, id)
	isCatchup := false

	if channel, exists := getPluginChannel(id); exists {
		// Use absolute URL for player to ensure it works across different contexts
		hostURL := c.BaseURL()
		// Always serve .m3u8 URL without quality parameter - let the player's qsel handle quality selection
		play_url = fmt.Sprintf("%s/%s.m3u8", hostURL, channel.URL)
	} else if getChannelCatchupSupport(id) {
		// Try to find current program for catchup-live experience
		// Check today (0) and yesterday (-1) in case program started before midnight
		offsets := []int{0, -1}
		found := false

		now := time.Now().UnixMilli()
		loc := time.FixedZone("IST", 5*3600+30*60)

		for _, offset := range offsets {
			if found {
				break
			}
			epg, err := getCatchupEPG(id, offset)
			if err != nil {
				continue
			}

			for _, prog := range epg {
				start, ok1 := prog["startEpoch"].(int64)
				end, ok2 := prog["endEpoch"].(int64)
				if ok1 && ok2 {
					// Scale to ms if needed
					if start < 100000000000 {
						start *= 1000
					}
					if end < 100000000000 {
						end *= 1000
					}

					if now >= start && now < end {
						// Found current program
						srno := ""
						if s, ok := prog["srno"].(string); ok {
							srno = s
						} else if s, ok := prog["srno"].(float64); ok {
							srno = fmt.Sprintf("%.0f", s)
						}

						// Format times to YYYYMMDDTHHMMSS
						startStr := time.UnixMilli(start).In(loc).Format("20060102T150405")
						endStr := time.UnixMilli(end).In(loc).Format("20060102T150405")

						play_url = fmt.Sprintf("/catchup/stream/%s?start=%s&end=%s&srno=%s", id, startStr, endStr, srno)
						isCatchup = true
						found = true
						utils.Log.Printf("Switched to catchup stream for live channel %s: %s", id, play_url)
						break
					}
				}
			}
		}
		if !found {
			utils.Log.Printf("Could not find current program in EPG for channel %s, falling back to live stream", id)
		}
	} else {
		utils.Log.Printf("Channel %s does not support catchup, using live stream", id)
	}

	internalUtils.SetCacheHeader(c, 3600)
	return c.Render("views/player_hls", fiber.Map{
		"play_url":   play_url,
		"is_catchup": isCatchup,
	})
}

// FaviconHandler Responds for favicon.ico request
func FaviconHandler(c *fiber.Ctx) error {
	return c.Redirect("/static/favicon.ico", fiber.StatusMovedPermanently)
}

// PlaylistHandler is the route for generating M3U playlist only
// For user convenience, redirect to /channels?type=m3u
func PlaylistHandler(c *fiber.Ctx) error {
	quality := c.Query("q")
	splitCategory := c.Query("c")
	languages := c.Query("l")
	skipGenres := c.Query("sg")
	return c.Redirect("/channels?type=m3u&q="+quality+"&c="+splitCategory+"&l="+languages+"&sg="+skipGenres, fiber.StatusMovedPermanently)
}

// ImageHandler loads image from JioTV server
func ImageHandler(c *fiber.Ctx) error {
	url := "https://jiotv.catchup.cdn.jio.com/dare_images/images/" + c.Params("file")
	return internalUtils.ProxyRequest(c, url, TV.Client, REQUEST_USER_AGENT)
}

func DASHTimeHandler(c *fiber.Ctx) error {
	return c.SendString(time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
}

// ChannelPlaylistRedirectHandler redirects to the export URL with the correct filename
func ChannelPlaylistRedirectHandler(c *fiber.Ctx) error {
	id := c.Params("id")

	// Fetch all channels to find the specific one
	apiResponse, err := television.Channels()
	if err != nil {
		return ErrorMessageHandler(c, err)
	}

	var targetChannel *television.Channel
	for i := range apiResponse.Result {
		if apiResponse.Result[i].ID == id {
			targetChannel = &apiResponse.Result[i]
			break
		}
	}

	if targetChannel == nil {
		return internalUtils.NotFoundError(c, "Channel not found")
	}

	// Sanitize filename
	filename := strings.ReplaceAll(targetChannel.Name, " ", "_")
	filename = strings.ReplaceAll(filename, "/", "-")
	filename = fmt.Sprintf("%s.m3u8", filename)

	// Redirect to the export URL with the filename and ID
	return c.Redirect(fmt.Sprintf("/playlist/export/%s?id=%s", filename, id), fiber.StatusFound)
}

// ChannelPlaylistExportHandler generates M3U playlist for a single channel
func ChannelPlaylistExportHandler(c *fiber.Ctx) error {
	id := c.Query("id")
	if id == "" {
		// Fallback: try to get ID from filename param if query is missing
		// This handles cases where the ID is passed as the filename (e.g. /playlist/export/143.m3u8)
		val := c.Params("filename")
		if val != "" {
			id = strings.TrimSuffix(val, ".m3u8")
		}
	}

	quality := c.Query("q")

	// Fetch all channels to find the specific one
	apiResponse, err := television.Channels()
	if err != nil {
		return ErrorMessageHandler(c, err)
	}

	var targetChannel *television.Channel
	for i := range apiResponse.Result {
		if apiResponse.Result[i].ID == id {
			targetChannel = &apiResponse.Result[i]
			break
		}
	}

	if targetChannel == nil {
		return internalUtils.NotFoundError(c, "Channel not found")
	}

	hostURL := strings.ToLower(c.Protocol()) + "://" + c.Get("Host")
	m3uContent := "#EXTM3U x-tvg-url=\"" + hostURL + "/epg.xml.gz\"\n"

	var channelURL string
	if quality != "" {
		channelURL = fmt.Sprintf("%s/live/%s/%s.m3u8", hostURL, quality, targetChannel.ID)
	} else {
		channelURL = fmt.Sprintf("%s/live/%s.m3u8", hostURL, targetChannel.ID)
	}

	// Logo handling
	var channelLogoURL string
	if strings.HasPrefix(targetChannel.LogoURL, "http://") || strings.HasPrefix(targetChannel.LogoURL, "https://") {
		channelLogoURL = targetChannel.LogoURL
	} else {
		channelLogoURL = fmt.Sprintf("%s/jtvimage/%s", hostURL, targetChannel.LogoURL)
	}

	// Group Title
	groupTitle := television.CategoryMap[targetChannel.Category]

	// Catchup attributes
	var catchupAttrs string
	if targetChannel.IsCatchupAvailable {
		catchupSource := fmt.Sprintf("%s/catchup/render/%s?start=${start}&end=${stop}", hostURL, targetChannel.ID)
		catchupAttrs = fmt.Sprintf(` catchup="append" catchup-days="7" catchup-source="%s"`, catchupSource)
	}

	m3uContent += fmt.Sprintf("#EXTINF:-1 tvg-id=%q tvg-name=%q tvg-logo=%q tvg-language=%q tvg-type=%q group-title=%q%s, %s\n%s\n",
		targetChannel.ID, targetChannel.Name, channelLogoURL, television.LanguageMap[targetChannel.Language], television.CategoryMap[targetChannel.Category], groupTitle, catchupAttrs, targetChannel.Name, channelURL)

	// Sanitize filename for Content-Disposition (double check)
	filename := strings.ReplaceAll(targetChannel.Name, " ", "_")
	filename = strings.ReplaceAll(filename, "/", "-")

	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.m3u8\"", filename))
	c.Set("Content-Type", "application/vnd.apple.mpegurl")
	return c.SendString(m3uContent)
}
