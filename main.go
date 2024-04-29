package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	rl "github.com/gen2brain/raylib-go/raylib"
)

var (
	WINDOW_WIDTH   = 1000
	WINDOW_HEIGHT  = 450
	RULER_Y        = 40
	BODY_Y         = 60
	HELP_Y_PADDING = 50
	PAD_X          = 40

	FONT_SIZE_HEADER = 30
	FONT_SIZE_BODY   = 25
	FONT_SIZE_HELP   = 20

	HEADERS = []string{"PRs", "Issues", "Alerts"}

	COLOR_BLUE_BG = rl.NewColor(91, 206, 250, 100)
	COLOR_PINK_BG = rl.NewColor(245, 169, 184, 100)
	COLOR_BLACK   = rl.NewColor(0, 0, 0, 255)
	COLOR_GRAY    = rl.NewColor(150, 150, 150, 255)

	COLOR_HEADER          = COLOR_BLACK
	COLOR_SELECTED_HEADER = COLOR_BLUE_BG
	COLOR_SELECTED_ITEM   = COLOR_BLUE_BG
	COLOR_RULER           = COLOR_GRAY
	COLOR_ITEM            = COLOR_BLACK
	COLOR_HELP            = COLOR_BLACK
)

type Config struct {
	Repos       []Repo
	Alerts      AlertsConfig
	GithubToken string
}

type AlertsConfig struct {
	Server   string
	Receiver string
}

type Repo struct {
	Owner string
	Name  string
}

func (r Repo) String() string {
	return fmt.Sprintf("%s/%s", r.Owner, r.Name)
}

func buildConfig(filename string) (Config, error) {
	contents, err := os.ReadFile(filename)
	if err != nil {
		return Config{}, fmt.Errorf("Could not open file: %s", err.Error())
	}
	var config struct {
		Repos  []string `json:"repos"`
		Alerts struct {
			Server   string `json:"server"`
			Receiver string `json:"receiver"`
		} `json:"alerts"`
	}
	if err := json.Unmarshal(contents, &config); err != nil {
		return Config{}, fmt.Errorf("Could not parse config: %s", err.Error())
	}
	var repos []Repo
	for _, repo := range config.Repos {
		split := strings.Split(repo, "/")
		if len(split) != 2 {
			return Config{}, fmt.Errorf("Incorrect repo format, should be `owner/name`, got %s\n", repo)
		}
		repos = append(repos, Repo{Owner: split[0], Name: split[1]})
	}
	return Config{
		Repos:       repos,
		Alerts:      AlertsConfig(config.Alerts),
		GithubToken: os.Getenv("GH_TOKEN"),
	}, nil
}

type State struct {
	SelectedHeader string
	SelectedItem   int
	Data           map[string][]Item
}

func NewState() State {
	return State{
		SelectedHeader: HEADERS[0],
		SelectedItem:   0,
		Data:           make(map[string][]Item),
	}
}

type Data map[string][]Item

type Item struct {
	Value       string
	URL         string
	Application string
}

func main() {
	config, err := buildConfig("config.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not parse config file: %s", err.Error())
		os.Exit(1)
	}
	state := NewState()
	go updateData(&state, config)

	if os.Getenv("LOG") == "false" {
		rl.SetTraceLogLevel(rl.LogNone)
	}
	rl.SetTargetFPS(60)
	rl.SetConfigFlags(rl.FlagWindowResizable)
	rl.InitWindow(int32(WINDOW_WIDTH), int32(WINDOW_HEIGHT), "Daeshboard")
	headerFont := rl.LoadFontEx("JetBrainsMonoNerdFont-Medium.ttf", 2*int32(FONT_SIZE_HEADER), nil, 256)
	bodyFont := rl.LoadFontEx("JetBrainsMonoNerdFont-Medium.ttf", 2*int32(FONT_SIZE_BODY), nil, 256)
	helpFont := rl.LoadFontEx("JetBrainsMonoNerdFont-Medium.ttf", 2*int32(FONT_SIZE_HELP), nil, 256)
	defer rl.CloseWindow()

	for !rl.WindowShouldClose() {
		rl.BeginDrawing()
		rl.ClearBackground(rl.RayWhite)

		shouldClose := reactToInput(&state)

		drawHeaders(state, headerFont, float32(FONT_SIZE_HEADER))
		drawRuler()
		drawBody(state, bodyFont, float32(FONT_SIZE_BODY))
		drawHelp(helpFont, float32(FONT_SIZE_HELP))

		rl.EndDrawing()
		if shouldClose {
			break
		}
	}
}

func updateData(state *State, config Config) {
	for {
		// TODO: Handle multiple pages in github responses
		prs, err := getPrs(config.Repos, config.GithubToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get pull requests: %s\n", err.Error())
			os.Exit(1)
		}
		state.Data["PRs"] = prs
		issues, err := getIssues(config.Repos, config.GithubToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get issues: %s\n", err.Error())
			os.Exit(1)
		}
		state.Data["Issues"] = issues
		alerts, err := getAlerts(config.Alerts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get alerts: %s\n", err.Error())
			os.Exit(1)
		}
		state.Data["Alerts"] = alerts
		time.Sleep(10 * time.Second)
	}
}

func getPrs(repos []Repo, token string) ([]Item, error) {
	var prs []Item
	var body []struct {
		Title   string `json:"title"`
		HtmlURL string `json:"html_url"`
	}
	for _, r := range repos {
		url := fmt.Sprintf("https://api.github.com/repos/%s/pulls", r)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return []Item{}, fmt.Errorf("Could not build request to get pull requests: %s", err.Error())
		}
		if token != "" {
			req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return []Item{}, fmt.Errorf("Could not get pull requests for repo %s: %s\n", r, err.Error())
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return []Item{}, fmt.Errorf("Got non-200 status code when getting pull request for repo %s: %s\n", r, resp.Status)
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return []Item{}, fmt.Errorf("Could not parse pull request response for repo %s: %s", r, err.Error())
		}
		for _, pr := range body {
			prs = append(prs, Item{
				Value: fmt.Sprintf("%s: %s", r, pr.Title),
				URL:   pr.HtmlURL,
			})
		}
	}
	return prs, nil
}

func getIssues(repos []Repo, token string) ([]Item, error) {
	var issues []Item
	var body []struct {
		Title       string `json:"title"`
		HtmlURL     string `json:"html_url"`
		PullRequest struct {
			URL string `json:"url"`
		} `json:"pull_request"`
	}
	for _, r := range repos {
		url := fmt.Sprintf("https://api.github.com/repos/%s/issues", r)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return []Item{}, fmt.Errorf("Could not build request to get pull requests: %s", err.Error())
		}
		if token != "" {
			req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return []Item{}, fmt.Errorf("Could not get issues for repo %s: %s\n", r, err.Error())
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return []Item{}, fmt.Errorf("Got non-200 status code when getting issues for repo %s: %s\n", r, resp.Status)
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return []Item{}, fmt.Errorf("Could not parse issue response for repo %s: %s", r, err.Error())
		}
		for _, issue := range body {
			// The issues endpoint returns pull requests as well, see
			// https://docs.github.com/en/rest/issues/issues?apiVersion=2022-11-28#list-repository-issues
			if issue.PullRequest.URL == "" {
				issues = append(issues, Item{
					Value: fmt.Sprintf("%s: %s", r, issue.Title),
					URL:   issue.HtmlURL,
				})
			}
		}
	}
	return issues, nil
}

func getAlerts(alertsConfig AlertsConfig) ([]Item, error) {
	var body []struct {
		Annotations struct {
			Description string `json:"description"`
		} `json:"annotations"`
	}
	query := fmt.Sprintf("receiver=%s&silenced=false&inhibited=false", url.QueryEscape(alertsConfig.Receiver))
	url := fmt.Sprintf("%s/api/v2/alerts?%s", alertsConfig.Server, query)
	resp, err := http.Get(url)
	if err != nil {
		return []Item{}, fmt.Errorf("Could not get alerts: %s\n", err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return []Item{}, fmt.Errorf("Got non-200 status code when getting alerts: %s\n", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return []Item{}, fmt.Errorf("Could not parse alerts response: %s", err.Error())
	}
	var alerts []Item
	for _, a := range body {
		alerts = append(alerts, Item{
			Value: a.Annotations.Description,
			URL:   fmt.Sprintf("%s/#/alerts?%s", alertsConfig.Server, query),
		})
	}
	return alerts, nil
}

func reactToInput(state *State) bool {
	shouldClose := false
	nItems := len(state.Data[state.SelectedHeader])
	switch rl.GetKeyPressed() {
	case rl.KeyLeft, rl.KeyA, rl.KeyH:
		index := slices.Index(HEADERS, state.SelectedHeader)
		newIndex := max(0, index-1)
		if newIndex != index {
			state.SelectedHeader = HEADERS[newIndex]
			state.SelectedItem = 0
		}
	case rl.KeyRight, rl.KeyD, rl.KeyL:
		index := slices.Index(HEADERS, state.SelectedHeader)
		newIndex := min(len(HEADERS)-1, index+1)
		if newIndex != index {
			state.SelectedHeader = HEADERS[newIndex]
			state.SelectedItem = 0
		}
	case rl.KeyUp, rl.KeyW, rl.KeyK:
		state.SelectedItem = max(0, state.SelectedItem-1)
	case rl.KeyDown, rl.KeyS, rl.KeyJ:
		state.SelectedItem = min(nItems-1, state.SelectedItem+1)
	case rl.KeyEnter, rl.KeySpace:
		openApplication(*state)
	case rl.KeyOne:
		state.SelectedHeader = HEADERS[0]
	case rl.KeyTwo:
		state.SelectedHeader = HEADERS[1]
	case rl.KeyThree:
		state.SelectedHeader = HEADERS[2]
	case rl.KeyQ:
		shouldClose = true
	}
	return shouldClose
}

func openApplication(state State) {
	// TODO: Default app or url to open when there are no items?
	if len(state.Data[state.SelectedHeader]) == 0 {
		return
	}
	item := state.Data[state.SelectedHeader][state.SelectedItem]
	if item.Application != "" {
		cmd := exec.Command("open", "-a", item.Application)
		cmd.Run()
	} else if item.URL != "" {
		rl.OpenURL(item.URL)
	}
}

func drawHeaders(state State, font rl.Font, fontSize float32) {
	rects := getHeaderRects(len(HEADERS))
	selectedHeaderIndex := slices.Index(HEADERS, state.SelectedHeader)
	for i, rect := range rects {
		if i == selectedHeaderIndex {
			rl.DrawRectangleRounded(rect, 1, 1, COLOR_SELECTED_HEADER)
		}
		header := HEADERS[i]
		nItems := len(state.Data[header])
		text := fmt.Sprintf("%s [%d]", header, nItems)
		textWidth := rl.MeasureText(text, int32(FONT_SIZE_HEADER))
		padX := (rect.Width - float32(textWidth)) / 2
		rl.DrawTextEx(font, text, rl.NewVector2(rect.X+padX, rect.Y), fontSize, 0, COLOR_HEADER)
	}
}

func drawRuler() {
	width := rl.GetScreenWidth()
	rl.DrawRectangle(0, int32(RULER_Y), int32(width), 1, COLOR_RULER)
}

func drawBody(state State, font rl.Font, fontSize float32) {
	data := state.Data[state.SelectedHeader]
	for i, d := range data {
		y := BODY_Y + i*(FONT_SIZE_BODY+5)
		if i == state.SelectedItem {
			textWidth := rl.MeasureText(d.Value, int32(FONT_SIZE_BODY))
			padding := float32(10)
			rect := rl.NewRectangle(float32(PAD_X)-padding, float32(y), float32(textWidth)+2*padding, float32(FONT_SIZE_BODY))
			rl.DrawRectangleRounded(rect, 1, 1, COLOR_SELECTED_ITEM)
		}
		rl.DrawTextEx(font, d.Value, rl.NewVector2(float32(PAD_X), float32(y)), fontSize, 0, COLOR_ITEM)
	}
}

func drawHelp(font rl.Font, fontSize float32) {
	text := fmt.Sprintf(`<hjkl, wasd, arrows, 1..%d> MOVE    <enter, space> OPEN    <q> QUIT`, len(HEADERS))
	textWidth := rl.MeasureText(text, int32(FONT_SIZE_HELP))
	x := (rl.GetScreenWidth() - int(textWidth)) / 2
	y := rl.GetScreenHeight() - HELP_Y_PADDING
	rect := rl.NewRectangle(float32(x), float32(y), float32(textWidth), float32(FONT_SIZE_HELP))
	rl.DrawRectangleRounded(rect, 1, 1, COLOR_PINK_BG)
	rl.DrawTextEx(font, text, rl.NewVector2(float32(x), float32(y)), fontSize, 0, COLOR_HELP)
}

func getHeaderRects(nHeaders int) []rl.Rectangle {
	y := 10
	width := rl.GetScreenWidth()
	headerWidth := (width - 2*PAD_X) / nHeaders
	headerHeight := FONT_SIZE_HEADER
	var positions []rl.Rectangle
	for i := range nHeaders {
		x := PAD_X + i*headerWidth
		positions = append(positions, rl.NewRectangle(float32(x), float32(y), float32(headerWidth), float32(headerHeight)))
	}
	return positions
}
