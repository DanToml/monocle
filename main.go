package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/gizak/termui"
	"github.com/jszwedko/go-circleci"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const (
	BANNER = `👁  monocle

	A simple terminal client for viewing the state of your CircleCI builds.
	`
)

var (
	circleciToken      string
	circleciInstallURL string
	updateInterval     string
)

func init() {
	flag.StringVar(&circleciToken, "circle-token", os.Getenv("CIRCLECI_TOKEN"), "CircleCI API Token, or ENV(CIRCLECI_TOKEN)")
	flag.StringVar(&updateInterval, "update-interval", "30s", "Update updateInterval")

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, BANNER)
		flag.PrintDefaults()
	}

	flag.Parse()
}

type projectInfo struct {
	user, projectName, branch string
}

func parseProjectInfo() (*projectInfo, error) {
	getOriginCmd := exec.Command("git", "config", "--get", "remote.origin.url")
	var originOutput bytes.Buffer
	getOriginCmd.Stdout = &originOutput
	if err := getOriginCmd.Run(); err != nil {
		return nil, err
	}

	getCurrentBranchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	var branchOutput bytes.Buffer
	getCurrentBranchCmd.Stdout = &branchOutput
	if err := getCurrentBranchCmd.Run(); err != nil {
		return nil, err
	}

	originStr := originOutput.String()
	branchStr := branchOutput.String()

	re := regexp.MustCompile("[a-zA-Z0-9]*\\.[a-zA-Z0-9]*(?::|\\/)(?P<Org>[a-zA-Z0-9\\-\\_]*)\\/(?P<Repo>[a-zA-Z0-9\\-\\_]*)")

	matches := re.FindStringSubmatch(originStr)

	return &projectInfo{
		user:        strings.Trim(matches[1], " \n\t"),
		projectName: strings.Trim(matches[2], " \n\t"),
		branch:      strings.Trim(branchStr, " \n\t"),
	}, nil
}

type buildData struct {
	JobName  string
	BuildNum string
	Status   string
	URL      string
	Duration string
}

func loadBuilds(info *projectInfo) []buildData {
	client := &circleci.Client{Token: circleciToken}
	builds, _ := client.ListRecentBuildsForProject(info.user, info.projectName, info.branch, "", 30, 0)

	data := []buildData{}

	for _, b := range builds {
		jobName := "build"
		if b.JobName != nil {
			jobName = *b.JobName
		} else if b.Workflows != nil && b.Workflows.JobName != "" {
			jobName = b.Workflows.JobName
		}

		duration := "n/a"

		if b.StartTime != nil && b.StopTime != nil {
			diff := b.StopTime.Sub(*b.StartTime)
			duration = fmt.Sprint(diff)
		} else if b.StartTime != nil && b.StopTime == nil {
			diff := time.Now().Sub(*b.StartTime)
			duration = fmt.Sprint(diff)
		}

		data = append(data, buildData{
			JobName:  jobName,
			BuildNum: fmt.Sprintf("%d", b.BuildNum),
			Status:   b.Status,
			Duration: duration,
			URL:      b.BuildURL,
		})
	}

	return data
}

func runCircleCIView() (*termui.Table, error) {
	info, err := parseProjectInfo()
	if err != nil {
		return nil, err
	}
	table := termui.NewTable()
	rows := [][]string{
		{"build_num", "job", "state", "duration", "url"},
	}

	builds := loadBuilds(info)
	redRows := []int{}
	greenRows := []int{}
	for i, b := range builds {
		if b.Status == "failed" {
			redRows = append(redRows, i+1)
		}
		if b.Status == "fixed" || b.Status == "success" {
			greenRows = append(greenRows, i+1)
		}

		rows = append(rows, []string{b.BuildNum, b.JobName, b.Status, b.Duration, b.URL})
	}

	table.Rows = rows
	table.FgColor = termui.ColorYellow
	table.BgColor = termui.ColorDefault
	table.TextAlign = termui.AlignLeft
	table.Border = true
	table.Separator = true
	table.Block.BorderLabel = fmt.Sprintf("builds for %s/%s/tree/%s", info.user, info.projectName, info.branch)
	table.Analysis()
	table.SetSize()

	table.FgColors[0] = termui.ColorWhite

	for _, br := range redRows {
		table.FgColors[br] = termui.ColorRed
	}
	for _, br := range greenRows {
		table.FgColors[br] = termui.ColorGreen
	}

	return table, nil
}

func setupCircleCIView() {
	table, err := runCircleCIView()
	if err != nil {
		log.Fatal(err)
	}
	if table != nil {
		termui.Render(table)
	}
}

func main() {
	if len(circleciToken) == 0 {
		log.Fatalf("a circleci token is required")
	}

	var ticker *time.Ticker

	dur, err := time.ParseDuration(updateInterval)
	if err != nil {
		log.Fatalf("parsing %s as duration failed: %v", updateInterval, err)
	}

	ticker = time.NewTicker(dur)

	if err := termui.Init(); err != nil {
		log.Fatalf("initializing termui failed: %v", err)
	}
	defer termui.Close()

	go setupCircleCIView()

	// press q to quit
	termui.Handle("/sys/kbd/q", func(termui.Event) {
		ticker.Stop()
		termui.StopLoop()
	})

	// Or press ctrl-c.
	termui.Handle("/sys/kbd/C-c", func(termui.Event) {
		ticker.Stop()
		termui.StopLoop()
	})

	termui.Handle("/sys/wnd/resize", func(e termui.Event) {
		termui.Clear()
		setupCircleCIView()
		termui.Render()
	})

	// Update on an interval
	go func() {
		for range ticker.C {
			setupCircleCIView()
		}
	}()

	// Start the loop.
	termui.Loop()
}
