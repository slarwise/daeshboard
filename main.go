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
	SelectedTab int
	Tabs        []Tab
	Data        map[string]HeaderData
	ShouldClose bool
}

type Tab struct {
	Title        string
	SelectedItem int
	LastViewedAt time.Time
}

type TabState struct {
	Selected     bool
	SelectedItem int
	Unread       bool
}

func NewState() State {
	tabs := []Tab{
		{Title: "PRs"},
		{Title: "Issues"},
		{Title: "Alerts"},
	}
	return State{
		Data: make(map[string]HeaderData),
		Tabs: tabs,
	}
}

type HeaderData struct {
	Items      []Item
	ModifiedAt time.Time
}

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
	windowTitle := "Daeshboard"
	rl.InitWindow(int32(WINDOW_WIDTH), int32(WINDOW_HEIGHT), windowTitle)
	headerFont := rl.LoadFontEx("JetBrainsMonoNerdFont-Medium.ttf", 2*int32(FONT_SIZE_HEADER), nil, 256)
	bodyFont := rl.LoadFontEx("JetBrainsMonoNerdFont-Medium.ttf", 2*int32(FONT_SIZE_BODY), nil, 256)
	helpFont := rl.LoadFontEx("JetBrainsMonoNerdFont-Medium.ttf", 2*int32(FONT_SIZE_HELP), nil, 256)
	defer rl.CloseWindow()

	lastModifiedAt := map[string]time.Time{
		"PRs":    time.Now(),
		"Issues": time.Now(),
		"Alerts": time.Now(),
	}
	for !rl.WindowShouldClose() && !state.ShouldClose {
		rl.BeginDrawing()
		rl.ClearBackground(rl.RayWhite)
		// Read the input and update the state
		// Draw stuff based on the state

		// Notifications:
		//     Show in the window title if any of the tabs was last viewed before its data was modified
		//     Show in the tab titles if the tab was last viewed before its data was modified
		//     Send a notification whenever any of the data's last modified at was updated

		reactToInput(&state)

		drawWindowTitle(&state)
		drawHeaders(state, headerFont, float32(FONT_SIZE_HEADER))
		drawRuler()
		drawBody(state, bodyFont, float32(FONT_SIZE_BODY))
		drawHelp(helpFont, float32(FONT_SIZE_HELP))

		notifyIfNeeded(&state, lastModifiedAt)

		rl.EndDrawing()
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
		if !slices.Equal(prs, state.Data["PRs"].Items) {
			fmt.Println("New PR!")
			state.Data["PRs"] = HeaderData{
				Items:      prs,
				ModifiedAt: time.Now(),
			}
		}
		issues, err := getIssues(config.Repos, config.GithubToken)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get issues: %s\n", err.Error())
			os.Exit(1)
		}
		if !slices.Equal(issues, state.Data["Issues"].Items) {
			fmt.Println("New issue!")
			state.Data["Issues"] = HeaderData{
				Items:      issues,
				ModifiedAt: time.Now(),
			}
		}
		alerts, err := getAlerts(config.Alerts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get alerts: %s\n", err.Error())
			os.Exit(1)
		}
		if !slices.Equal(alerts, state.Data["Alerts"].Items) {
			fmt.Println("New alert!")
			state.Data["Alerts"] = HeaderData{
				Items:      alerts,
				ModifiedAt: time.Now(),
			}
		}
		time.Sleep(10 * time.Second)
	}
}

type PR struct {
	Title     string    `json:"title"`
	HtmlURL   string    `json:"html_url"`
	CreatedAt time.Time `json:"created_at"`
}

func getPrs(repos []Repo, token string) ([]Item, error) {
	var items []Item
	var prs []PR
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
		if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
			return []Item{}, fmt.Errorf("Could not parse pull request response for repo %s: %s", r, err.Error())
		}
		slices.SortFunc(prs, func(a, b PR) int {
			return -1 * a.CreatedAt.Compare(b.CreatedAt)
		})
		for _, pr := range prs {
			items = append(items, Item{
				Value: fmt.Sprintf("%s: %s", r, pr.Title),
				URL:   pr.HtmlURL,
			})
		}
	}
	return items, nil
}

type Issue struct {
	Title       string `json:"title"`
	HtmlURL     string `json:"html_url"`
	PullRequest struct {
		URL string `json:"url"`
	} `json:"pull_request"`
	CreatedAt time.Time `json:"created_at"`
}

func getIssues(repos []Repo, token string) ([]Item, error) {
	var items []Item
	var issues []Issue
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
		if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
			return []Item{}, fmt.Errorf("Could not parse issue response for repo %s: %s", r, err.Error())
		}
		slices.SortFunc(issues, func(a, b Issue) int {
			return -1 * a.CreatedAt.Compare(b.CreatedAt)
		})
		for _, issue := range issues {
			// The issues endpoint returns pull requests as well, see
			// https://docs.github.com/en/rest/issues/issues?apiVersion=2022-11-28#list-repository-issues
			if issue.PullRequest.URL == "" {
				items = append(items, Item{
					Value: fmt.Sprintf("%s: %s", r, issue.Title),
					URL:   issue.HtmlURL,
				})
			}
		}
	}
	return items, nil
}

type Alert struct {
	Annotations struct {
		Description string `json:"description"`
	} `json:"annotations"`
	StartsAt time.Time `json:"startsAt"`
}

func getAlerts(alertsConfig AlertsConfig) ([]Item, error) {
	var alerts []Alert
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
	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		return []Item{}, fmt.Errorf("Could not parse alerts response: %s", err.Error())
	}
	slices.SortFunc(alerts, func(a, b Alert) int {
		return -1 * a.StartsAt.Compare(b.StartsAt)
	})
	var items []Item
	for _, a := range alerts {
		items = append(items, Item{
			Value: a.Annotations.Description,
			URL:   fmt.Sprintf("%s/#/alerts?%s", alertsConfig.Server, query),
		})
	}
	return items, nil
}

func reactToInput(state *State) {
	gotInput := true
	nItems := len(state.Data[state.Tabs[state.SelectedTab].Title].Items)
	switch rl.GetKeyPressed() {
	case rl.KeyLeft, rl.KeyA, rl.KeyH:
		newSelectedTab := max(0, state.SelectedTab-1)
		if newSelectedTab != state.SelectedTab {
			state.SelectedTab = newSelectedTab
		}
	case rl.KeyRight, rl.KeyD, rl.KeyL:
		newSelectedTab := min(len(state.Tabs)-1, state.SelectedTab+1)
		if newSelectedTab != state.SelectedTab {
			state.SelectedTab = newSelectedTab
		}
	case rl.KeyUp, rl.KeyW, rl.KeyK:
		state.Tabs[state.SelectedTab].SelectedItem = max(0, state.Tabs[state.SelectedTab].SelectedItem-1)
	case rl.KeyDown, rl.KeyS, rl.KeyJ:
		state.Tabs[state.SelectedTab].SelectedItem = min(nItems-1, state.Tabs[state.SelectedTab].SelectedItem+1)
	case rl.KeyEnter, rl.KeySpace:
		openApplication(*state)
	case rl.KeyOne:
		state.SelectedTab = 0
	case rl.KeyTwo:
		state.SelectedTab = 1
	case rl.KeyThree:
		state.SelectedTab = 2
	case rl.KeyQ:
		state.ShouldClose = true
	default:
		gotInput = false
	}
	if gotInput {
		state.Tabs[state.SelectedTab].LastViewedAt = time.Now()
	}
}

func openApplication(state State) {
	// TODO: Default app or url to open when there are no items?
	if len(state.Data[state.Tabs[state.SelectedTab].Title].Items) == 0 {
		return
	}
	item := state.Data[state.Tabs[state.SelectedTab].Title].Items[state.Tabs[state.SelectedTab].SelectedItem]
	if item.Application != "" {
		cmd := exec.Command("open", "-a", item.Application)
		cmd.Run()
	} else if item.URL != "" {
		rl.OpenURL(item.URL)
	}
}

func drawWindowTitle(state *State) {
	for _, t := range state.Tabs {
		if t.LastViewedAt.Before(state.Data[t.Title].ModifiedAt) {
			rl.SetWindowTitle("● Daeshboard")
			return
		}
	}
	rl.SetWindowTitle("Daeshboard")
}

func drawHeaders(state State, font rl.Font, fontSize float32) {
	rects := getHeaderRects(len(HEADERS))
	for i, tab := range state.Tabs {
		if i == state.SelectedTab {
			rl.DrawRectangleRounded(rects[i], 1, 1, COLOR_SELECTED_HEADER)
		}
		nItems := len(state.Data[tab.Title].Items)
		notice := ""

		if tab.LastViewedAt.Before(state.Data[tab.Title].ModifiedAt) {
			notice = "*"
		}
		text := fmt.Sprintf("%s%s [%d]", notice, tab.Title, nItems)
		textWidth := rl.MeasureText(text, int32(FONT_SIZE_HEADER))
		padX := (rects[i].Width - float32(textWidth)) / 2
		rl.DrawTextEx(font, text, rl.NewVector2(rects[i].X+padX, rects[i].Y), fontSize, 0, COLOR_HEADER)
	}
}

func notifyIfNeeded(state *State, lastModifiedAt map[string]time.Time) {
	// Send a notification whenever any of the data's last modified at was updated
	for key, t := range lastModifiedAt {
		if t.Before(state.Data[key].ModifiedAt) {
			lastModifiedAt[key] = state.Data[key].ModifiedAt
			if err := Notify(); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to create notification: %s\n", err.Error())
				os.Exit(1)
			}
		}
	}

}

func Notify() error {
	osa, err := exec.LookPath("osascript")
	if err != nil {
		return err
	}

	script := fmt.Sprintf("display notification %q with title %q", "Something happened", "Daeshboard")
	cmd := exec.Command(osa, "-e", script)
	return cmd.Run()
}

func drawRuler() {
	width := rl.GetScreenWidth()
	rl.DrawRectangle(0, int32(RULER_Y), int32(width), 1, COLOR_RULER)
}

func drawBody(state State, font rl.Font, fontSize float32) {
	selectedTab := state.Tabs[state.SelectedTab]
	data := state.Data[selectedTab.Title]
	for i, d := range data.Items {
		y := BODY_Y + i*(FONT_SIZE_BODY+5)
		if i == selectedTab.SelectedItem {
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
