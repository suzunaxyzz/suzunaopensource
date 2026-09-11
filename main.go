package main

import (
	"archive/zip"
	"bufio"
	"crypto/tls"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var LauncherVersion = "1.3.1"

//go:embed cacert.pem
var caCertPem []byte

const (
	DefaultBaseURL    = "https://suzuna.xyz"
	LauncherUserAgent = "RobloxPlayerLauncher/1.0 (Suzuna)"
	ClientDownloadURL = DefaultBaseURL + "/downloads/clients"
)

func newHttpClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
}

func newCookieClient() *http.Client {
	c := newHttpClient()
	jar, _ := cookiejar.New(nil)
	c.Jar = jar
	return c
}

func httpGetWithUA(client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", LauncherUserAgent)
	return client.Do(req)
}

type PlaceLauncherResponse struct {
	JobId         string `json:"jobId"`
	Status        int    `json:"status"`
	JoinScriptUrl string `json:"joinScriptUrl"`
	Message       string `json:"message"`
	JoinScript    any    `json:"joinScript"`
	Errors        []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

var logWriter io.Writer = os.Stdout

func logPrintln(a ...any)               { fmt.Fprintln(logWriter, a...) }
func logPrintf(format string, a ...any) { fmt.Fprintf(logWriter, format, a...) }

func setupLogging() *os.File {
	logPath := filepath.Join(os.TempDir(), "suzuna-launcher.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil
	}
	logWriter = io.MultiWriter(os.Stdout, f)
	logPrintf("\n=== Suzuna Launcher v%s starting %s (args=%v) ===\n", LauncherVersion, time.Now().Format(time.RFC3339), os.Args)
	return f
}

var consoleVisible bool

func pause() {
	if !consoleVisible {
		return
	}
	logPrintln("\nPress Enter to close this window...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func fail(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	logPrintln(msg)
	if consoleVisible {
		pause()
	} else {
		showErrorMessageBox("Suzuna Launcher", msg)
	}
	os.Exit(1)
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			fail("Suzuna Launcher crashed: %v", r)
		}
	}()

	var (
		registerFlag   = flag.Bool("register", false, "Register suzuna-player URI protocol in Windows Registry")
		unregisterFlag = flag.Bool("unregister", false, "Remove suzuna-player URI protocol from Windows Registry")
		testFlag       = flag.Bool("test", false, "Run end-to-end diagnostic test")
		placeFlag      = flag.Int64("place", 0, "Place ID to launch directly")
		joinFlag       = flag.String("join", "", "Direct Join script URL")
		ticketFlag     = flag.String("ticket", "", "Authentication ticket for client launch")
		clientFlag     = flag.String("client", "", "Path to the client executable (auto-detected if omitted)")
		serverFlag     = flag.String("server", DefaultBaseURL, "Base server URL (e.g. https://suzuna.xyz)")
		yearFlag       = flag.String("year", "2018", "Client version year (e.g. 2017, 2018, 2020)")
		versionFlag    = flag.Bool("version", false, "Print launcher version")
		quietFlag      = flag.Bool("quiet", false, "Never show a console or dialog, even on error (for installer use)")
	)
	flag.Parse()

	args := flag.Args()
	isProtocolURI := len(args) > 0 && (strings.HasPrefix(args[0], "suzuna-player:") || strings.HasPrefix(args[0], "suzuna:") || strings.HasPrefix(args[0], "rexursclient:"))
	isGameLaunch := *joinFlag != "" || *placeFlag > 0 || isProtocolURI
	isBareLaunch := len(os.Args) <= 1

	consoleVisible = attachConsole()
	if !consoleVisible && !isGameLaunch && !isBareLaunch && !*quietFlag {
		consoleVisible = allocConsole()
	}

	if f := setupLogging(); f != nil {
		defer f.Close()
	}
	defer pause()

	if *versionFlag {
		logPrintf("Suzuna Launcher v%s (%s/%s)\n", LauncherVersion, runtime.GOOS, runtime.GOARCH)
		return
	}

	if *registerFlag {
		if err := registerProtocol(); err != nil {
			fail("Failed to register protocol: %v", err)
		}
		logPrintln("Successfully registered suzuna-player: protocol in Windows Registry.")
		return
	}

	if *unregisterFlag {
		if err := unregisterProtocol(); err != nil {
			fail("Failed to remove protocol registration: %v", err)
		}
		logPrintln("Removed suzuna-player: protocol registration.")
		return
	}

	if *testFlag {
		runSelfTest(*serverFlag, *placeFlag, *ticketFlag)
		return
	}

	if *joinFlag != "" {
		launchClient(*clientFlag, *ticketFlag, *joinFlag, *yearFlag)
		return
	}

	if *placeFlag > 0 {
		joinUrl, err := requestPlace(*serverFlag, *placeFlag, *ticketFlag)
		if err != nil {
			fail("Error requesting place %d: %v", *placeFlag, err)
		}
		launchClient(*clientFlag, *ticketFlag, joinUrl, *yearFlag)
		return
	}

	if isProtocolURI {
		handleProtocolURI(args[0], *clientFlag)
		return
	}

	if runtime.GOOS == "windows" && isBareLaunch {
		installAndRegister()
		return
	}

	logPrintln("Suzuna Game Launcher v" + LauncherVersion)
	logPrintln("Usage:")
	logPrintln("  SuzunaLauncher.exe suzuna-player:1+launchmode:play+gameinfo:<ticket>+placelauncherurl:<url>")
	logPrintln("  SuzunaLauncher.exe --register                  Register protocol handler")
	logPrintln("  SuzunaLauncher.exe --place <placeId>          Launch specific place")
	logPrintln("  SuzunaLauncher.exe --test                     Run diagnostic self-test")
	logPrintln("")
	logPrintln("Registering protocol handler now...")
	if err := registerProtocol(); err != nil {
		logPrintf("Registration error: %v\n", err)
	} else {
		logPrintln("Registration successful!")
	}
}

func handleProtocolURI(rawURI string, clientOverride string) {
	logPrintf("[Launcher] Received protocol URI: %s\n", rawURI)

	var placeLauncherUrl string
	var ticket string
	year := "2018"

	if strings.Contains(rawURI, "+") {
		payload := rawURI
		for _, pfx := range []string{"suzuna-player:", "suzuna:", "rexursclient:"} {
			if strings.HasPrefix(strings.ToLower(payload), pfx) {
				payload = payload[len(pfx):]
				break
			}
		}
		payload = strings.Trim(payload, "\"'/")

		params := parseProtocolParams(payload)
		placeLauncherUrl = params["placelauncherurl"]
		ticket = params["gameinfo"]
		clientVer := params["clientversion"]
		if strings.Contains(clientVer, "2017") {
			year = "2017"
		} else if strings.Contains(clientVer, "2021") {
			year = "2021"
		} else if strings.Contains(clientVer, "2020") {
			year = "2020"
		} else if strings.Contains(clientVer, "2018") {
			year = "2018"
		}
	} else if u, err := url.Parse(rawURI); err == nil {
		q := u.Query()
		placeId := q.Get("place")
		if placeId == "" {
			placeId = q.Get("placeId")
		}
		ticket = q.Get("ticket")
		if ticket == "" {
			ticket = q.Get("gameinfo")
		}
		if q.Get("2020") == "true" {
			year = "2020"
		} else if q.Get("2018") == "true" {
			year = "2018"
		} else if q.Get("2017") == "true" {
			year = "2017"
		} else if y := q.Get("year"); y != "" {
			year = y
		}
		if pUrl := q.Get("placelauncherurl"); pUrl != "" {
			placeLauncherUrl = pUrl
		} else if placeId != "" {
			placeLauncherUrl = fmt.Sprintf("%s/Game/PlaceLauncher.ashx?request=RequestGame&placeId=%s&isPartyLeader=false&gender=&isTeleport=true", DefaultBaseURL, placeId)
		}
	}

	if placeLauncherUrl == "" {
		fail("[Launcher] Error: missing placelauncherurl in URI payload")
	}

	if unescaped, err := url.QueryUnescape(placeLauncherUrl); err == nil {
		placeLauncherUrl = unescaped
	}

	logPrintf("[Launcher] Handing PlaceLauncher URL to client: %s (year=%s)\n", placeLauncherUrl, year)
	launchClient(clientOverride, ticket, placeLauncherUrl, year)
}

func parseProtocolParams(payload string) map[string]string {
	result := make(map[string]string)
	parts := strings.Split(payload, "+")
	for _, part := range parts {
		colonIdx := strings.Index(part, ":")
		if colonIdx != -1 {
			key := strings.ToLower(strings.TrimSpace(part[:colonIdx]))
			val := strings.TrimSpace(part[colonIdx+1:])
			result[key] = val
		}
	}
	return result
}

func negotiateSession(client *http.Client, baseURLOrFullURL, ticket string) error {
	parsed, err := url.Parse(baseURLOrFullURL)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("could not determine server host from %q", baseURLOrFullURL)
	}
	negotiateUrl := fmt.Sprintf("%s://%s/login/negotiate.ashx?suggest=%s", parsed.Scheme, parsed.Host, url.QueryEscape(ticket))
	logPrintln("[Launcher] Negotiating session...")
	resp, err := httpGetWithUA(client, negotiateUrl)
	if err != nil {
		return fmt.Errorf("negotiate request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("negotiate failed with status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func pollPlaceLauncher(client *http.Client, initialURL string) (string, error) {
	reqURL := initialURL
	for attempt := 1; attempt <= 30; attempt++ {
		logPrintf("[Launcher] Polling game status (attempt %d/30)...\n", attempt)
		resp, err := httpGetWithUA(client, reqURL)
		if err != nil {
			return "", fmt.Errorf("HTTP request failed: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}

		var plr PlaceLauncherResponse
		if err := json.Unmarshal(body, &plr); err != nil {
			return "", fmt.Errorf("invalid JSON response: %w (body: %s)", err, string(body))
		}

		if len(plr.Errors) > 0 {
			return "", fmt.Errorf(plr.Errors[0].Message)
		}

		switch plr.Status {
		case 2:
			if plr.JoinScriptUrl != "" {
				return plr.JoinScriptUrl, nil
			}
			return "", fmt.Errorf("server ready but joinScriptUrl is empty")
		case 1:
			logPrintf("[Launcher] Server starting (job: %s), waiting...\n", plr.JobId)
			parsed, err := url.Parse(initialURL)
			if err == nil && plr.JobId != "" {
				q := parsed.Query()
				q.Set("request", "CheckGameJobStatus")
				q.Set("jobId", plr.JobId)
				parsed.RawQuery = q.Encode()
				reqURL = parsed.String()
			}
			time.Sleep(1500 * time.Millisecond)
		case 0:
			fallthrough
		default:
			msg := plr.Message
			if msg == "" {
				msg = fmt.Sprintf("PlaceLauncher returned status %d", plr.Status)
			}
			return "", fmt.Errorf(msg)
		}
	}

	return "", fmt.Errorf("timeout waiting for game server after 30 attempts")
}

func requestPlace(server string, placeId int64, ticket string) (string, error) {
	client := newCookieClient()
	if ticket != "" {
		if err := negotiateSession(client, server, ticket); err != nil {
			return "", fmt.Errorf("negotiate failed: %w", err)
		}
	}
	reqUrl := fmt.Sprintf("%s/Game/PlaceLauncher.ashx?request=RequestGame&placeId=%d", server, placeId)
	return pollPlaceLauncher(client, reqUrl)
}

var knownClientExeNames = []string{
	"ProjectXPlayerBeta",
	"RexursPlayerBeta",
	"SuzunaPlayerBeta",
	"KoronePlayerBeta",
}

func yearDirCandidates(year string) []string {
	if year == "" {
		return nil
	}
	return []string{year, year + "L", year + "M"}
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func findRobloxClient(override string, targetYear string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err == nil {
			return override, nil
		}
		return "", fmt.Errorf("specified client executable not found: %s", override)
	}

	suffix := exeSuffix()
	var names []string
	for _, n := range knownClientExeNames {
		names = append(names, n+suffix)
	}

	roots := []string{"."}
	if runtime.GOOS == "windows" {
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			for _, vendor := range []string{"Pekora", "Suzuna", "Rexurs", "Korone"} {
				roots = append(roots, filepath.Join(localAppData, vendor))
			}
		}
	}

	if targetYear != "" {
		for _, root := range roots {
			for _, name := range names {
				for _, yd := range yearDirCandidates(targetYear) {
					candidates := []string{
						filepath.Join(root, "Versions", "suzuna-client", yd, name),
						filepath.Join(root, yd, name),
					}
					for _, c := range candidates {
						if st, err := os.Stat(c); err == nil && !st.IsDir() {
							abs, _ := filepath.Abs(c)
							return abs, nil
						}
					}
					if matches, _ := filepath.Glob(filepath.Join(root, "Versions", "*", yd, name)); len(matches) > 0 {
						abs, _ := filepath.Abs(matches[0])
						return abs, nil
					}
				}
			}
		}
	}

	for _, root := range roots {
		for _, name := range names {
			candidates := []string{
				filepath.Join(root, name),
				filepath.Join(root, "Versions", name),
			}
			for _, c := range candidates {
				if st, err := os.Stat(c); err == nil && !st.IsDir() {
					abs, _ := filepath.Abs(c)
					return abs, nil
				}
			}
			if matches, _ := filepath.Glob(filepath.Join(root, "Versions", "*", name)); len(matches) > 0 {
				abs, _ := filepath.Abs(matches[0])
				return abs, nil
			}
			if matches, _ := filepath.Glob(filepath.Join(root, "Versions", "*", "*", name)); len(matches) > 0 {
				abs, _ := filepath.Abs(matches[0])
				return abs, nil
			}
			for _, year := range []string{"2018", "2017", "2020", "2021", "2016"} {
				c := filepath.Join(root, year, name)
				if st, err := os.Stat(c); err == nil && !st.IsDir() {
					abs, _ := filepath.Abs(c)
					return abs, nil
				}
			}
		}
	}

	genericPattern := "*PlayerBeta" + suffix
	for _, root := range roots {
		globs := []string{
			filepath.Join(root, genericPattern),
			filepath.Join(root, "Versions", genericPattern),
			filepath.Join(root, "Versions", "*", genericPattern),
			filepath.Join(root, "Versions", "*", "*", genericPattern),
		}
		for _, g := range globs {
			if matches, _ := filepath.Glob(g); len(matches) > 0 {
				abs, _ := filepath.Abs(matches[0])
				return abs, nil
			}
		}
	}

	return "", fmt.Errorf("could not find a client executable (tried %s). Place one in the launcher folder or specify with --client", strings.Join(names, ", "))
}

func serverBaseFromURL(fullURL string) string {
	if u, err := url.Parse(fullURL); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	return DefaultBaseURL
}

func clientZipTag(year string) string {
	switch year {
	case "2017":
		return "2017L"
	case "2018":
		return "2018L"
	case "2020":
		return "2020L"
	case "2021":
		return "2021M"
	default:
		return year + "L"
	}
}

func clientInstallRoot() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "Pekora", "Versions")
}

func downloadAndInstallClient(year string) error {
	tag := clientZipTag(year)
	zipURL := ClientDownloadURL + "/pekora-" + tag + ".zip"
	dest := clientInstallRoot()
	logPrintf("[Launcher] Downloading client %s from %s\n", tag, zipURL)

	tmp, err := os.CreateTemp("", "suzuna-client-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	req, err := http.NewRequest("GET", zipURL, nil)
	if err != nil {
		tmp.Close()
		return err
	}
	req.Header.Set("User-Agent", LauncherUserAgent)
	resp, err := client.Do(req)
	if err != nil {
		tmp.Close()
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		tmp.Close()
		return fmt.Errorf("server returned HTTP %d for %s", resp.StatusCode, zipURL)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("saving download failed: %w", err)
	}
	tmp.Close()

	logPrintf("[Launcher] Extracting client to %s\n", dest)
	if err := extractZip(tmpPath, dest); err != nil {
		return fmt.Errorf("extract failed: %w", err)
	}
	logPrintf("[Launcher] Client %s installed.\n", tag)
	return nil
}

func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	cleanDest := filepath.Clean(destDir)
	for _, f := range r.File {
		fpath := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(fpath, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("unsafe path in archive: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fpath, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}
		out, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			out.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func launchClient(clientOverride string, ticket string, joinUrl string, year string) {
	base := serverBaseFromURL(joinUrl)
	negotiateUrl := base + "/Login/Negotiate.ashx"

	clientPath, err := findRobloxClient(clientOverride, year)
	if err != nil && clientOverride == "" {
		logPrintf("[Launcher] No client installed, downloading it (one-time)...\n")
		if derr := downloadAndInstallClient(year); derr != nil {
			fail("Couldn't download the Suzuna client automatically.\n\n%v\n\nInstall it manually from:\n%s/pekora-%s.zip", derr, ClientDownloadURL, clientZipTag(year))
		}
		clientPath, err = findRobloxClient(clientOverride, year)
	}
	if err != nil {
		logPrintf("[Launcher] Command line to manually run client:\n")
		logPrintf("<Player>Beta.exe -a %q -t %q -j %q\n", negotiateUrl, ticket, joinUrl)
		fail("[Launcher] %v", err)
	}

	appSettings := filepath.Join(filepath.Dir(clientPath), "AppSettings.xml")
	var xmlContent string
	if year == "2018" {
		xmlContent = fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?> <Settings> <BaseUrl>%s</BaseUrl> </Settings>", base)
	} else if year == "2020" || year == "2021" {
		xmlContent = fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?><Settings><ContentFolder>content</ContentFolder><BaseUrl>%s</BaseUrl></Settings>", base)
	} else {
		xmlContent = fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\r\n<Settings>\r\n\t<ContentFolder>content</ContentFolder>\r\n\t<BaseUrl>%s</BaseUrl>\r\n</Settings>\r\n", base)
	}
	_ = os.WriteFile(appSettings, []byte(xmlContent), 0644)
	_ = os.MkdirAll(filepath.Join(filepath.Dir(clientPath), "content"), 0755)

	caPath := filepath.Join(filepath.Dir(clientPath), "cacert.pem")
	if len(caCertPem) > 0 {
		_ = os.WriteFile(caPath, caCertPem, 0644)
	}

	logPrintf("[Launcher] Launching: %s\n", clientPath)
	args := []string{
		"-a", negotiateUrl,
		"-j", joinUrl,
	}
	if ticket != "" {
		args = append(args, "-t", ticket)
	}

	cmd := exec.Command(clientPath, args...)
	cmd.Dir = filepath.Dir(clientPath)
	cmd.Env = append(os.Environ(),
		"CURL_CA_BUNDLE="+caPath,
		"SSL_CERT_FILE="+caPath,
		"SSL_CERT_DIR="+filepath.Dir(clientPath),
	)
	hideChildWindow(cmd)
	if err := cmd.Start(); err != nil {
		fail("[Launcher] Failed to start client: %v", err)
	}

	logPrintf("[Launcher] Client process started successfully (PID: %d)\n", cmd.Process.Pid)

	rpcPath := filepath.Join(filepath.Dir(clientPath), "..", "RexursRPC.exe")
	if _, err := os.Stat(rpcPath); os.IsNotExist(err) {
		rpcPath = filepath.Join(filepath.Dir(clientPath), "RexursRPC.exe")
	}
	if _, err := os.Stat(rpcPath); err == nil && cmd.Process != nil {
		rpcCmd := exec.Command(rpcPath, "--pid", fmt.Sprintf("%d", cmd.Process.Pid), "--year", year)
		hideChildWindow(rpcCmd)
		_ = rpcCmd.Start()
	}
}

func registerProtocol() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("protocol registration is only supported on Windows")
	}

	exePath, err := os.Executable()
	if err != nil {
		return err
	}

	openCmd := fmt.Sprintf("\"%s\" \"%%1\"", exePath)

	protocols := []string{"suzuna-player", "suzuna", "rexursclient"}
	for _, proto := range protocols {
		commands := [][]string{
			{"add", `HKCU\Software\Classes\` + proto, "/ve", "/d", "URL:Suzuna Protocol", "/f"},
			{"add", `HKCU\Software\Classes\` + proto, "/v", "URL Protocol", "/d", "", "/f"},
			{"add", `HKCU\Software\Classes\` + proto + `\shell\open\command`, "/ve", "/d", openCmd, "/f"},
		}

		for _, args := range commands {
			cmd := exec.Command("reg", args...)
			hideChildWindow(cmd)
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("reg command failed: %s (%w)", string(out), err)
			}
		}
	}

	return nil
}

func unregisterProtocol() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("protocol registration is only supported on Windows")
	}
	protocols := []string{"suzuna-player", "suzuna", "rexursclient"}
	for _, proto := range protocols {
		cmd := exec.Command("reg", "delete", `HKCU\Software\Classes\`+proto, "/f")
		hideChildWindow(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			if !strings.Contains(string(out), "unable to find") {
				return fmt.Errorf("reg command failed: %s (%w)", string(out), err)
			}
		}
	}
	return nil
}

func installAndRegister() {
	if err := registerProtocol(); err != nil {
		showErrorMessageBox("Suzuna Launcher",
			"Could not register the game handler:\n\n"+err.Error()+
				"\n\nTry running the launcher again as your normal user.")
		return
	}

	showInfoMessageBox("Suzuna Launcher",
		"Suzuna Launcher is installed!\n\n"+
			"Open "+DefaultBaseURL+", pick a game, and click PLAY.\n"+
			"Your browser will hand off to the launcher automatically.")
	openURL(DefaultBaseURL)
}

func runSelfTest(server string, placeId int64, ticket string) {
	if placeId <= 0 {
		placeId = 2
	}

	logPrintln("==================================================")
	logPrintf(" Suzuna Launcher Self-Test (Server: %s, Place: %d)\n", server, placeId)
	logPrintln("==================================================")

	client := newCookieClient()
	if ticket != "" {
		if err := negotiateSession(client, server, ticket); err != nil {
			logPrintf("Negotiate failed: %v\n", err)
		}
	}

	fmt.Print("[1/4] Testing Web Server Connection... ")
	resp, err := httpGetWithUA(client, server+"/health/live")
	if err == nil && resp.StatusCode == 200 {
		logPrintln("OK (HTTP 200)")
	} else if err == nil {
		logPrintf("OK (HTTP %d)\n", resp.StatusCode)
	} else {
		resp2, err2 := httpGetWithUA(client, server)
		if err2 == nil {
			logPrintf("OK (HTTP %d)\n", resp2.StatusCode)
		} else {
			logPrintf("FAILED (%v)\n", err)
		}
	}

	logPrintf("[2/4] Testing PlaceLauncher Request for Place %d... \n", placeId)
	reqUrl := fmt.Sprintf("%s/Game/PlaceLauncher.ashx?request=RequestGame&placeId=%d", server, placeId)
	joinUrl, err := pollPlaceLauncher(client, reqUrl)
	if err != nil {
		logPrintf("      FAILED: %v\n", err)
	} else {
		logPrintf("      SUCCESS! Received JoinScript URL: %s\n", joinUrl)

		fmt.Print("[3/4] Downloading and Verifying JoinScript... ")
		jsResp, err := httpGetWithUA(client, joinUrl)
		if err != nil {
			logPrintf("FAILED (%v)\n", err)
		} else {
			defer jsResp.Body.Close()
			jsBody, _ := io.ReadAll(jsResp.Body)
			if len(jsBody) > 0 {
				logPrintf("OK (%d bytes)\n", len(jsBody))
				lines := strings.Split(string(jsBody), "\n")
				preview := ""
				for i := 0; i < len(lines) && i < 3; i++ {
					preview += "      | " + lines[i] + "\n"
				}
				fmt.Print(preview)
			} else {
				logPrintln("FAILED (empty response)")
			}
		}
	}

	fmt.Print("[4/4] Detecting Local Roblox Player Client... ")
	clientPath, err := findRobloxClient("", "2018")
	if err != nil {
		logPrintf("NOT FOUND (%v)\n", err)
		logPrintln("      (Standard: place RobloxPlayerBeta.exe next to SuzunaLauncher.exe)")
	} else {
		logPrintf("FOUND (%s)\n", clientPath)
	}

	logPrintln("==================================================")
	logPrintln(" Diagnostic Complete.")
	logPrintln("==================================================")
}
