package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"
)

const (
	defaultRedirectURI = "http://127.0.0.1:8888/callback"
	configDirName      = "spotify-cli"
	configFileName     = "config.json"
	tokenEndpoint      = "https://accounts.spotify.com/api/token"
	authorizeEndpoint  = "https://accounts.spotify.com/authorize"
	apiBaseURL         = "https://api.spotify.com/v1"
)

var scopes = []string{
	"user-read-playback-state",
	"user-modify-playback-state",
	"user-read-currently-playing",
}

type config struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri"`
	DeviceID     string `json:"device_id,omitempty"`
	Token        token  `json:"token"`
}

type token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	Expiry       time.Time `json:"expiry"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

type devicesResponse struct {
	Devices []device `json:"devices"`
}

type device struct {
	ID             string `json:"id"`
	IsActive       bool   `json:"is_active"`
	IsPrivate      bool   `json:"is_private_session"`
	IsRestricted   bool   `json:"is_restricted"`
	Name           string `json:"name"`
	SupportsVolume bool   `json:"supports_volume"`
	Type           string `json:"type"`
	VolumePercent  int    `json:"volume_percent"`
}

type searchResponse struct {
	Tracks struct {
		Items []track `json:"items"`
	} `json:"tracks"`
	Albums struct {
		Items []album `json:"items"`
	} `json:"albums"`
	Playlists struct {
		Items []playlist `json:"items"`
	} `json:"playlists"`
}

type track struct {
	Name    string `json:"name"`
	URI     string `json:"uri"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name string `json:"name"`
	} `json:"album"`
}

type album struct {
	Name    string `json:"name"`
	URI     string `json:"uri"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	AlbumType string `json:"album_type"`
}

type playlist struct {
	Name  string `json:"name"`
	URI   string `json:"uri"`
	Owner struct {
		DisplayName string `json:"display_name"`
		ID          string `json:"id"`
	} `json:"owner"`
}

func Run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	switch args[0] {
	case "auth":
		return cmdAuth(args[1:])
	case "devices":
		return cmdDevices()
	case "search":
		return cmdSearch(args[1:])
	case "play":
		return cmdPlay(args[1:])
	case "pause":
		return cmdPause()
	case "next":
		return cmdNext()
	case "use-device":
		return cmdUseDevice(args[1:])
	case "help", "-h", "--help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage() {
	fmt.Println(`sp-cli

Small Spotify CLI for terminal-first playback control.

Usage:
  sp-cli auth [client-id] [client-secret]
  sp-cli devices
  sp-cli use-device <device-id>
  sp-cli search [--type track|album|playlist|all] <query>
  sp-cli play [spotify:track:...|spotify:album:...|spotify:playlist:...]
  sp-cli pause
  sp-cli next

Examples:
  sp-cli auth
  sp-cli devices
  sp-cli search "宇多田ヒカル First Love"
  sp-cli search --type album "宇多田ヒカル"
  sp-cli play spotify:track:39HrUxcvKF3jtLz7fUDWXc
  sp-cli play spotify:album:2CVV8PtUYYsux8XOzWkCP0
  sp-cli play spotify:playlist:37i9dQZF1DXcBWIGoYBM5M

Environment:
  SPOTIFY_CLIENT_ID
  SPOTIFY_CLIENT_SECRET
  SPOTIFY_REDIRECT_URI
  SPOTIFY_DEVICE_ID

Notes:
  - auth opens the browser, receives the callback, and stores tokens in ~/.config/spotify-cli/config.json
  - access tokens are refreshed automatically
  - search defaults to track, album, and playlist results
  - play without an argument resumes current playback`)
}

func cmdAuth(args []string) error {
	cfg, err := loadConfig()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if len(args) > 0 {
		cfg.ClientID = args[0]
	}
	if len(args) > 1 {
		cfg.ClientSecret = args[1]
	}
	if cfg.ClientID == "" {
		cfg.ClientID = os.Getenv("SPOTIFY_CLIENT_ID")
	}
	if cfg.ClientSecret == "" {
		cfg.ClientSecret = os.Getenv("SPOTIFY_CLIENT_SECRET")
	}
	if cfg.RedirectURI == "" {
		cfg.RedirectURI = os.Getenv("SPOTIFY_REDIRECT_URI")
	}
	if cfg.RedirectURI == "" {
		cfg.RedirectURI = defaultRedirectURI
	}
	if cfg.ClientID == "" {
		clientID, err := promptLine("Client ID: ")
		if err != nil {
			return err
		}
		cfg.ClientID = clientID
	}
	if cfg.ClientSecret == "" {
		clientSecret, err := promptSecret("Client Secret: ")
		if err != nil {
			return err
		}
		cfg.ClientSecret = clientSecret
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return errors.New("client id and client secret are required")
	}

	code, err := authorize(cfg.ClientID, cfg.RedirectURI)
	if err != nil {
		return err
	}

	tok, err := exchangeCode(cfg.ClientID, cfg.ClientSecret, cfg.RedirectURI, code)
	if err != nil {
		return err
	}
	cfg.Token = tok

	if err := saveConfig(cfg); err != nil {
		return err
	}

	fmt.Println("authenticated and saved tokens")
	return nil
}

func cmdDevices() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	client := newSpotifyClient(cfg)
	var resp devicesResponse
	if err := client.getJSON(apiBaseURL+"/me/player/devices", &resp); err != nil {
		return err
	}

	if len(resp.Devices) == 0 {
		fmt.Println("no devices found")
		return nil
	}

	preferred := preferredDeviceID(cfg)
	for _, d := range resp.Devices {
		marks := make([]string, 0, 2)
		if d.IsActive {
			marks = append(marks, "active")
		}
		if preferred != "" && d.ID == preferred {
			marks = append(marks, "preferred")
		}
		status := ""
		if len(marks) > 0 {
			status = " [" + strings.Join(marks, ", ") + "]"
		}
		fmt.Printf("%s | %s | %s%s\n", d.ID, d.Type, d.Name, status)
	}
	return nil
}

func promptLine(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	reader := bufio.NewReader(os.Stdin)
	value, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func promptSecret(label string) (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("client secret is required; set SPOTIFY_CLIENT_SECRET or run from a terminal to enter it interactively")
	}
	fmt.Fprint(os.Stderr, label)
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(secret)), nil
}

func cmdUseDevice(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: sp-cli use-device <device-id>")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfg.DeviceID = args[0]
	if err := saveConfig(cfg); err != nil {
		return err
	}
	fmt.Println("saved preferred device")
	return nil
}

func cmdSearch(args []string) error {
	query, searchTypes, err := parseSearchArgs(args)
	if err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	values := url.Values{}
	values.Set("q", query)
	values.Set("type", strings.Join(searchTypes, ","))
	values.Set("limit", "10")

	client := newSpotifyClient(cfg)
	var resp searchResponse
	if err := client.getJSON(apiBaseURL+"/search?"+values.Encode(), &resp); err != nil {
		return err
	}

	printed := false
	if containsSearchType(searchTypes, "track") && len(resp.Tracks.Items) > 0 {
		printed = true
		fmt.Println("tracks:")
		for i, t := range resp.Tracks.Items {
			fmt.Printf("%d. %s | %s | %s | %s\n", i+1, t.Name, joinArtistNames(t.Artists), t.Album.Name, t.URI)
		}
	}

	if containsSearchType(searchTypes, "album") && len(resp.Albums.Items) > 0 {
		printed = true
		fmt.Println("albums:")
		for i, a := range resp.Albums.Items {
			fmt.Printf("%d. %s | %s | %s | %s\n", i+1, a.Name, joinArtistNames(a.Artists), a.AlbumType, a.URI)
		}
	}

	if containsSearchType(searchTypes, "playlist") && len(resp.Playlists.Items) > 0 {
		printed = true
		fmt.Println("playlists:")
		for i, p := range resp.Playlists.Items {
			fmt.Printf("%d. %s | %s | %s\n", i+1, p.Name, playlistOwnerName(p.Owner), p.URI)
		}
	}

	if !printed {
		fmt.Println("no results found")
		return nil
	}
	return nil
}

func cmdPlay(args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	client := newSpotifyClient(cfg)
	deviceID, err := client.resolveDeviceID()
	if err != nil {
		return err
	}

	path := apiBaseURL + "/me/player/play?device_id=" + url.QueryEscape(deviceID)
	if len(args) == 0 {
		return client.doJSON(http.MethodPut, path, nil, nil)
	}

	body, err := playBody(args[0])
	if err != nil {
		return err
	}
	return client.doJSON(http.MethodPut, path, body, nil)
}

func cmdPause() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	client := newSpotifyClient(cfg)
	deviceID, err := client.resolveDeviceID()
	if err != nil {
		return err
	}
	path := apiBaseURL + "/me/player/pause?device_id=" + url.QueryEscape(deviceID)
	return client.doJSON(http.MethodPut, path, nil, nil)
}

func cmdNext() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	client := newSpotifyClient(cfg)
	deviceID, err := client.resolveDeviceID()
	if err != nil {
		return err
	}
	path := apiBaseURL + "/me/player/next?device_id=" + url.QueryEscape(deviceID)
	return client.doJSON(http.MethodPost, path, nil, nil)
}

type spotifyClient struct {
	cfg        *config
	httpClient *http.Client
}

func newSpotifyClient(cfg *config) *spotifyClient {
	return &spotifyClient{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *spotifyClient) getJSON(endpoint string, out any) error {
	return c.doJSON(http.MethodGet, endpoint, nil, out)
}

func (c *spotifyClient) doJSON(method, endpoint string, body any, out any) error {
	if err := c.ensureToken(); err != nil {
		return err
	}

	status, respBody, err := c.doRequest(method, endpoint, body)
	if err != nil {
		return err
	}
	if status == http.StatusUnauthorized && c.cfg.Token.RefreshToken != "" {
		if err := c.refreshToken(); err != nil {
			return err
		}
		status, respBody, err = c.doRequest(method, endpoint, body)
		if err != nil {
			return err
		}
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("spotify api returned %d: %s", status, strings.TrimSpace(string(respBody)))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return err
		}
	}
	return nil
}

func (c *spotifyClient) doRequest(method, endpoint string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, endpoint, reader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token.AccessToken)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, respBody, nil
}

func (c *spotifyClient) ensureToken() error {
	if c.cfg.Token.AccessToken == "" {
		return errors.New("missing access token, run `sp-cli auth`")
	}
	if time.Until(c.cfg.Token.Expiry) > 30*time.Second {
		return nil
	}
	if c.cfg.Token.RefreshToken == "" {
		return errors.New("access token expired and no refresh token is available")
	}
	return c.refreshToken()
}

func (c *spotifyClient) refreshToken() error {
	tok, err := refreshAccessToken(c.cfg.ClientID, c.cfg.ClientSecret, c.cfg.Token.RefreshToken)
	if err != nil {
		return err
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = c.cfg.Token.RefreshToken
	}
	c.cfg.Token = tok
	return saveConfig(c.cfg)
}

func (c *spotifyClient) resolveDeviceID() (string, error) {
	if envDevice := os.Getenv("SPOTIFY_DEVICE_ID"); envDevice != "" {
		return envDevice, nil
	}
	if c.cfg.DeviceID != "" {
		return c.cfg.DeviceID, nil
	}
	var resp devicesResponse
	if err := c.getJSON(apiBaseURL+"/me/player/devices", &resp); err != nil {
		return "", err
	}
	for _, d := range resp.Devices {
		if d.IsActive {
			return d.ID, nil
		}
	}
	if len(resp.Devices) == 0 {
		return "", errors.New("no spotify devices found")
	}
	return resp.Devices[0].ID, nil
}

func authorize(clientID, redirectURI string) (string, error) {
	redirectURL, err := url.Parse(redirectURI)
	if err != nil {
		return "", err
	}
	if redirectURL.Host == "" {
		return "", errors.New("redirect uri must include a host")
	}

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	server := &http.Server{}
	mux := http.NewServeMux()
	mux.HandleFunc(redirectURL.Path, func(w http.ResponseWriter, r *http.Request) {
		if errText := r.URL.Query().Get("error"); errText != "" {
			http.Error(w, "spotify authorization failed: "+errText, http.StatusBadRequest)
			select {
			case errCh <- errors.New(errText):
			default:
			}
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			select {
			case errCh <- errors.New("authorization callback missing code"):
			default:
			}
			return
		}
		_, _ = io.WriteString(w, "authorized, you can return to the terminal")
		select {
		case codeCh <- code:
		default:
		}
	})
	server.Handler = mux

	ln, err := net.Listen("tcp", redirectURL.Host)
	if err != nil {
		return "", err
	}
	defer ln.Close()

	go func() {
		if serveErr := server.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			select {
			case errCh <- serveErr:
			default:
			}
		}
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", strings.Join(scopes, " "))
	authURL := authorizeEndpoint + "?" + params.Encode()

	fmt.Println("open this URL if the browser does not launch:")
	fmt.Println(authURL)
	_ = openBrowser(authURL)

	select {
	case code := <-codeCh:
		return code, nil
	case err := <-errCh:
		return "", err
	case <-time.After(2 * time.Minute):
		return "", errors.New("timed out waiting for spotify auth callback")
	}
}

func exchangeCode(clientID, clientSecret, redirectURI, code string) (token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	return requestToken(form)
}

func refreshAccessToken(clientID, clientSecret, refreshToken string) (token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	return requestToken(form)
}

func requestToken(form url.Values) (token, error) {
	resp, err := http.PostForm(tokenEndpoint, form)
	if err != nil {
		return token{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return token{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return token{}, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return token{}, err
	}
	return token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tr.TokenType,
		Expiry:       time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}, nil
}

func openBrowser(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "linux":
		cmd = exec.Command("xdg-open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		return errors.New("unsupported platform for automatic browser open")
	}
	return cmd.Start()
}

func joinArtistNames(artists []struct {
	Name string `json:"name"`
}) string {
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		names = append(names, artist.Name)
	}
	return strings.Join(names, ", ")
}

func playlistOwnerName(owner struct {
	DisplayName string `json:"display_name"`
	ID          string `json:"id"`
}) string {
	if owner.DisplayName != "" {
		return owner.DisplayName
	}
	return owner.ID
}

func parseSearchArgs(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, errors.New("usage: sp-cli search [--type track|album|playlist|all] <query>")
	}

	searchType := "all"
	queryArgs := args
	if len(args) >= 2 && args[0] == "--type" {
		searchType = args[1]
		queryArgs = args[2:]
	}
	if len(queryArgs) == 0 {
		return "", nil, errors.New("usage: sp-cli search [--type track|album|playlist|all] <query>")
	}

	searchTypes, err := normalizeSearchTypes(searchType)
	if err != nil {
		return "", nil, err
	}
	return strings.Join(queryArgs, " "), searchTypes, nil
}

func normalizeSearchTypes(value string) ([]string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "all":
		return []string{"track", "album", "playlist"}, nil
	case "track":
		return []string{"track"}, nil
	case "album":
		return []string{"album"}, nil
	case "playlist":
		return []string{"playlist"}, nil
	default:
		return nil, fmt.Errorf("unsupported search type %q", value)
	}
}

func containsSearchType(searchTypes []string, target string) bool {
	for _, searchType := range searchTypes {
		if searchType == target {
			return true
		}
	}
	return false
}

func playBody(uri string) (map[string]any, error) {
	switch {
	case strings.HasPrefix(uri, "spotify:track:"):
		return map[string]any{"uris": []string{uri}}, nil
	case strings.HasPrefix(uri, "spotify:album:"), strings.HasPrefix(uri, "spotify:playlist:"):
		return map[string]any{"context_uri": uri}, nil
	default:
		return nil, fmt.Errorf("unsupported Spotify URI %q: expected track, album, or playlist", uri)
	}
}

func preferredDeviceID(cfg *config) string {
	if envDevice := os.Getenv("SPOTIFY_DEVICE_ID"); envDevice != "" {
		return envDevice
	}
	return cfg.DeviceID
}

func loadConfig() (*config, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &config{}, err
		}
		return nil, err
	}
	var cfg config
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, err
	}
	if cfg.RedirectURI == "" {
		cfg.RedirectURI = defaultRedirectURI
	}
	if cfg.ClientID == "" {
		cfg.ClientID = os.Getenv("SPOTIFY_CLIENT_ID")
	}
	if cfg.ClientSecret == "" {
		cfg.ClientSecret = os.Getenv("SPOTIFY_CLIENT_SECRET")
	}
	return &cfg, nil
}

func saveConfig(cfg *config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, payload, 0o600)
}

func configPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, configDirName, configFileName), nil
}
