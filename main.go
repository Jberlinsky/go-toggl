/*

Package toggl provides an API for interacting with the Toggl time tracking service.

See https://github.com/toggl/toggl_api_docs for more information on Toggl's REST API.

*/
package toggl

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Toggl service constants
const (
	TogglAPI       = "https://toggl.com/api/v8"
	ReportsAPI     = "https://toggl.com/reports/api/v2"
	DefaultAppName = "go-toggl"
)

var (
	dlog   = log.New(os.Stderr, "[toggl] ", log.LstdFlags)
	client = &http.Client{}

	// AppName is the application name used when creating timers.
	AppName = DefaultAppName
)

// structures ///////////////////////////

// Session represents an active connection to the Toggl REST API.
type Session struct {
	APIToken string
	username string
	password string
}

// Account represents a user account.
type Account struct {
	Data struct {
		APIToken        string      `json:"api_token"`
		Timezone        string      `json:"timezone"`
		ID              int         `json:"id"`
		Workspaces      []Workspace `json:"workspaces"`
		Clients         []Client    `json:"clients"`
		Projects        []Project   `json:"projects"`
		Tasks           []Task      `json:"tasks"`
		Tags            []Tag       `json:"tags"`
		TimeEntries     []TimeEntry `json:"time_entries"`
		BeginningOfWeek int         `json:"beginning_of_week"`
	} `json:"data"`
	Since int `json:"since"`
}

// Workspace represents a user workspace.
type Workspace struct {
	ID              int    `json:"id"`
	RoundingMinutes int    `json:"rounding_minutes"`
	Rounding        int    `json:"rounding"`
	Name            string `json:"name"`
	Premium         bool   `json:"premium"`
}

// Client represents a client.
type Client struct {
	Wid   int    `json:"wid"`
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Notes string `json:"notes"`
}

// Project represents a project.
type Project struct {
	Wid             int        `json:"wid"`
	ID              int        `json:"id"`
	Cid             int        `json:"cid"`
	Name            string     `json:"name"`
	Active          bool       `json:"active"`
	Billable        float32    `json:"billable"`
	ServerDeletedAt *time.Time `json:"server_deleted_at,omitempty"`
}

type Group struct {
	Wid  int    `json:"wid"`
	ID   int    `json:"id"`
	Name string `json:"name"`
	At   string `json:"at"`
}

// IsActive indicates whether a project exists and is active
func (p *Project) IsActive() bool {
	return p.Active && p.ServerDeletedAt == nil
}

// Task represents a task.
type Task struct {
	Wid  int    `json:"wid"`
	Pid  int    `json:"pid"`
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Tag represents a tag.
type Tag struct {
	Wid  int    `json:"wid"`
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// TimeEntry represents a single time entry.
type TimeEntry struct {
	Wid         int        `json:"wid,omitempty"`
	ID          int        `json:"id,omitempty"`
	Pid         int        `json:"pid"`
	Tid         int        `json:"tid"`
	Description string     `json:"description,omitempty"`
	Stop        *time.Time `json:"stop,omitempty"`
	Start       *time.Time `json:"start,omitempty"`
	Tags        []string   `json:"tags"`
	Duration    int64      `json:"duration,omitempty"`
	DurOnly     bool       `json:"duronly"`
	Billable    float32    `json:"billable"`
}

type DetailedTimeEntry struct {
	ID              int        `json:"id"`
	Pid             int        `json:"pid"`
	Tid             int        `json:"tid"`
	Uid             int        `json:"uid"`
	User            string     `json:"user,omitempty"`
	Description     string     `json:"description"`
	Project         string     `json:"project"`
	ProjectColor    string     `json:"project_color"`
	ProjectHexColor string     `json:"project_hex_color"`
	Client          string     `json:"client"`
	Start           *time.Time `json:"start"`
	End             *time.Time `json:"end"`
	Updated         *time.Time `json:"updated"`
	Duration        int64      `json:"dur"`
	Billable        float32    `json:"billable"`
	Tags            []string   `json:"tags"`
}

// SummaryReport represents a summary report generated by Toggl's reporting API.
type SummaryReport struct {
	TotalGrand int `json:"total_grand"`
	Data       []struct {
		ID    int `json:"id"`
		Time  int `json:"time"`
		Title struct {
			Project  string `json:"project"`
			Client   string `json:"client"`
			Color    string `json:"color"`
			HexColor string `json:"hex_color"`
		} `json:"title"`
		Items []struct {
			Title map[string]string `json:"title"`
			Time  int               `json:"time"`
		} `json:"items"`
	} `json:"data"`
}

// DetailedReport represents a summary report generated by Toggl's reporting API.
type DetailedReport struct {
	TotalGrand int                 `json:"total_grand"`
	TotalCount int                 `json:"total_count"`
	PerPage    int                 `json:"per_page"`
	Data       []DetailedTimeEntry `json:"data"`
}

// functions ////////////////////////////

// OpenSession opens a session using an existing API token.
func OpenSession(apiToken string) Session {
	return Session{APIToken: apiToken}
}

// NewSession creates a new session by retrieving a user's API token.
func NewSession(username, password string) (session Session, err error) {
	session.username = username
	session.password = password

	data, err := session.get(TogglAPI, "/me", nil)
	if err != nil {
		return session, err
	}

	var account Account
	err = decodeAccount(data, &account)
	if err != nil {
		return session, err
	}

	session.username = ""
	session.password = ""
	session.APIToken = account.Data.APIToken

	return session, nil
}

// GetAccount returns a user's account information, including a list of active
// projects and timers.
func (session *Session) GetAccount() (Account, error) {
	params := map[string]string{"with_related_data": "true"}
	data, err := session.get(TogglAPI, "/me", params)
	if err != nil {
		return Account{}, err
	}

	var account Account
	err = decodeAccount(data, &account)
	return account, err
}

func (session *Session) GetGroups(wid int) ([]Group, error) {
	path := fmt.Sprintf("/workspaces/%v/groups", wid)
	data, err := session.get(TogglAPI, path, nil)
	if err != nil {
		return []Group{}, err
	}
	var groups []Group
	err = decodeGroups(data, &groups)
	return groups, err
}

// GetSummaryReport retrieves a summary report using Toggle's reporting API.
func (session *Session) GetSummaryReport(workspace int, since, until string) (SummaryReport, error) {
	params := map[string]string{
		"user_agent":   "jc-toggl",
		"grouping":     "projects",
		"since":        since,
		"until":        until,
		"rounding":     "on",
		"workspace_id": fmt.Sprintf("%d", workspace)}
	data, err := session.get(ReportsAPI, "/summary", params)
	if err != nil {
		return SummaryReport{}, err
	}
	dlog.Printf("Got data: %s", data)

	var report SummaryReport
	err = decodeSummaryReport(data, &report)
	return report, err
}

type DetailedReportConfig struct {
	WorkspaceId int      `json:"workspace_id"`
	Since       string   `json:"since"`
	Until       string   `json:"until"`
	Page        int      `json:"page"`
	UserAgent   string   `json:"jc-toggl"`
	Rounding    string   `json:"rounding"`
	GroupIds    []string `json:"group_ids"`
}

// GetDetailedReport retrieves a detailed report using Toggle's reporting API.
func (session *Session) GetDetailedReport(config *DetailedReportConfig) (DetailedReport, error) {
	if config.UserAgent == "" {
		config.UserAgent = "jc-toggl"
	}

	if config.Rounding == "" {
		config.Rounding = "off"
	}

	params := map[string]string{
		"user_agent":           config.UserAgent,
		"since":                config.Since,
		"until":                config.Until,
		"page":                 fmt.Sprintf("%d", config.Page),
		"rounding":             config.Rounding,
		"workspace_id":         fmt.Sprintf("%d", config.WorkspaceId),
		"members_of_group_ids": strings.Join(config.GroupIds, ","),
	}
	data, err := session.get(ReportsAPI, "/details", params)
	if err != nil {
		return DetailedReport{}, err
	}
	dlog.Printf("Got data: %s", data)

	var report DetailedReport
	err = decodeDetailedReport(data, &report)
	return report, err
}

// StartTimeEntry creates a new time entry.
func (session *Session) StartTimeEntry(description string) (TimeEntry, error) {
	data := map[string]interface{}{
		"time_entry": map[string]string{
			"description":  description,
			"created_with": AppName,
		},
	}
	respData, err := session.post(TogglAPI, "/time_entries/start", data)
	return timeEntryRequest(respData, err)
}

// GetCurrentTimeEntry returns the current time entry, that's running
func (session *Session) GetCurrentTimeEntry() (TimeEntry, error) {
	data, err := session.get(TogglAPI, "/time_entries/current", nil)
	if err != nil {
		return TimeEntry{}, err
	}

	return timeEntryRequest(data, err)
}

// GetTimeEntries returns a list of time entries
func (session *Session) GetTimeEntries(startDate, endDate time.Time) ([]TimeEntry, error) {
	params := make(map[string]string)
	params["start_date"] = startDate.Format(time.RFC3339)
	params["end_date"] = endDate.Format(time.RFC3339)
	data, err := session.get(TogglAPI, "/time_entries", params)
	if err != nil {
		return nil, err
	}
	results := make([]TimeEntry, 0)
	err = json.Unmarshal(data, &results)
	if err != nil {
		return nil, err
	}
	return results, nil
}

// StartTimeEntryForProject creates a new time entry for a specific project. Note that the 'billable' option is only
// meaningful for Toggl Pro accounts; it will be ignored for free accounts.
func (session *Session) StartTimeEntryForProject(description string, projectID int, billable bool) (TimeEntry, error) {
	data := map[string]interface{}{
		"time_entry": map[string]interface{}{
			"description":  description,
			"pid":          projectID,
			"billable":     billable,
			"created_with": AppName,
		},
	}
	respData, err := session.post(TogglAPI, "/time_entries/start", data)
	return timeEntryRequest(respData, err)
}

// UpdateTimeEntry changes information about an existing time entry.
func (session *Session) UpdateTimeEntry(timer TimeEntry) (TimeEntry, error) {
	dlog.Printf("Updating timer %v", timer)
	data := map[string]interface{}{
		"time_entry": timer,
	}
	path := fmt.Sprintf("/time_entries/%v", timer.ID)
	respData, err := session.post(TogglAPI, path, data)
	return timeEntryRequest(respData, err)
}

// ContinueTimeEntry continues a time entry, either by creating a new entry
// with the same description or by extending the duration of an existing entry.
// In both cases the new entry will have the same description and project ID as
// the existing one.
func (session *Session) ContinueTimeEntry(timer TimeEntry, duronly bool) (TimeEntry, error) {
	dlog.Printf("Continuing timer %v", timer)
	var respData []byte
	var err error

	if duronly && time.Now().Local().Format("2006-01-02") == timer.Start.Local().Format("2006-01-02") {
		// If we're doing a duration-only continuation for a timer today, then
		// create a new entry that's a copy of the existing one with an
		// adjusted duration
		entry := timer.Copy()
		entry.Duration = -(time.Now().Unix() - entry.Duration)
		entry.DurOnly = true
		data := map[string]interface{}{
			"time_entry": entry,
		}
		path := fmt.Sprintf("/time_entries/%d", timer.ID)
		respData, err = session.put(TogglAPI, path, data)
	} else {
		// If we're not doing a duration-only continuation, or a duration timer
		// doesn't already exist for today, create a completely new time entry
		data := map[string]interface{}{
			"time_entry": map[string]interface{}{
				"description":  timer.Description,
				"pid":          timer.Pid,
				"tid":          timer.Tid,
				"billable":     timer.Billable,
				"created_with": AppName,
				"tags":         timer.Tags,
				"duronly":      duronly,
			},
		}
		respData, err = session.post(TogglAPI, "/time_entries/start", data)
	}
	return timeEntryRequest(respData, err)
}

// UnstopTimeEntry starts a new entry that is a copy of the given one, including
// the given timer's start time. The given time entry is then deleted.
func (session *Session) UnstopTimeEntry(timer TimeEntry) (newEntry TimeEntry, err error) {
	dlog.Printf("Unstopping timer %v", timer)
	var respData []byte

	data := map[string]interface{}{
		"time_entry": map[string]interface{}{
			"description":  timer.Description,
			"pid":          timer.Pid,
			"tid":          timer.Tid,
			"billable":     timer.Billable,
			"created_with": AppName,
			"tags":         timer.Tags,
			"duronly":      timer.DurOnly,
		},
	}

	if respData, err = session.post(TogglAPI, "/time_entries/start", data); err != nil {
		err = fmt.Errorf("New entry not started: %v", err)
		return
	}

	if newEntry, err = timeEntryRequest(respData, err); err != nil {
		err = fmt.Errorf("New entry not valid: %v", err)
		return
	}

	newEntry.Start = timer.Start

	if _, err = session.UpdateTimeEntry(newEntry); err != nil {
		err = fmt.Errorf("New entry not updated: %v", err)
		return
	}

	if _, err = session.DeleteTimeEntry(timer); err != nil {
		err = fmt.Errorf("Old entry not deleted: %v", err)
	}

	return
}

// StopTimeEntry stops a running time entry.
func (session *Session) StopTimeEntry(timer TimeEntry) (TimeEntry, error) {
	dlog.Printf("Stopping timer %v", timer)
	path := fmt.Sprintf("/time_entries/%v/stop", timer.ID)
	respData, err := session.put(TogglAPI, path, nil)
	return timeEntryRequest(respData, err)
}

// AddRemoveTag adds or removes a tag from the time entry corresponding to a
// given ID.
func (session *Session) AddRemoveTag(entryID int, tag string, add bool) (TimeEntry, error) {
	dlog.Printf("Adding tag to time entry %v", entryID)

	action := "add"
	if !add {
		action = "remove"
	}

	data := map[string]interface{}{
		"time_entry": map[string]interface{}{
			"tags":       []string{tag},
			"tag_action": action,
		},
	}
	path := fmt.Sprintf("/time_entries/%v", entryID)
	respData, err := session.post(TogglAPI, path, data)

	return timeEntryRequest(respData, err)
}

// DeleteTimeEntry deletes a time entry.
func (session *Session) DeleteTimeEntry(timer TimeEntry) ([]byte, error) {
	dlog.Printf("Deleting timer %v", timer)
	path := fmt.Sprintf("/time_entries/%v", timer.ID)
	return session.delete(TogglAPI, path)
}

// IsRunning returns true if the receiver is currently running.
func (e *TimeEntry) IsRunning() bool {
	return e.Duration < 0
}

// GetProjects allows to query for all projects in a workspace
func (session *Session) GetProjects(wid int) (projects []Project, err error) {
	dlog.Printf("Getting projects for workspace %d", wid)
	path := fmt.Sprintf("/workspaces/%v/projects", wid)
	data, err := session.get(TogglAPI, path, nil)
	if err != nil {
		return
	}

	err = json.Unmarshal(data, &projects)
	dlog.Printf("Unmarshaled '%s' into %#v\n", data, projects)
	return
}

// GetProjects allows to query for all projects in a workspace
func (session *Session) GetProject(id int) (project *Project, err error) {
	type dataProject struct {
		Data Project
	}
	dlog.Printf("Getting project with id %d", id)
	path := fmt.Sprintf("/projects/%v", id)
	data, err := session.get(TogglAPI, path, nil)
	if err != nil {
		return nil, err
	}
	var dProject dataProject
	err = json.Unmarshal(data, &dProject)
	dlog.Printf("Unmarshaled '%s' into %#v\n", data, dProject)
	return &dProject.Data, nil
}

// CreateProject creates a new project.
func (session *Session) CreateProject(name string, wid int) (proj Project, err error) {
	dlog.Printf("Creating project %s", name)
	data := map[string]interface{}{
		"project": map[string]interface{}{
			"name": name,
			"wid":  wid,
		},
	}

	respData, err := session.post(TogglAPI, "/projects", data)
	if err != nil {
		return proj, err
	}

	var entry struct {
		Data Project `json:"data"`
	}
	err = json.Unmarshal(respData, &entry)
	dlog.Printf("Unmarshaled '%s' into %#v\n", respData, entry)
	if err != nil {
		return proj, err
	}

	return entry.Data, nil
}

// UpdateProject changes information about an existing project.
func (session *Session) UpdateProject(project Project) (Project, error) {
	dlog.Printf("Updating project %v", project)
	data := map[string]interface{}{
		"project": project,
	}
	path := fmt.Sprintf("/projects/%v", project.ID)
	respData, err := session.put(TogglAPI, path, data)

	if err != nil {
		return Project{}, err
	}

	var entry struct {
		Data Project `json:"data"`
	}
	err = json.Unmarshal(respData, &entry)
	dlog.Printf("Unmarshaled '%s' into %#v\n", data, entry)
	if err != nil {
		return Project{}, err
	}

	return entry.Data, nil
}

// DeleteProject deletes a project.
func (session *Session) DeleteProject(project Project) ([]byte, error) {
	dlog.Printf("Deleting project %v", project)
	path := fmt.Sprintf("/projects/%v", project.ID)
	return session.delete(TogglAPI, path)
}

// CreateTag creates a new tag.
func (session *Session) CreateTag(name string, wid int) (proj Tag, err error) {
	dlog.Printf("Creating tag %s", name)
	data := map[string]interface{}{
		"tag": map[string]interface{}{
			"name": name,
			"wid":  wid,
		},
	}

	respData, err := session.post(TogglAPI, "/tags", data)
	if err != nil {
		return proj, err
	}

	var entry struct {
		Data Tag `json:"data"`
	}
	err = json.Unmarshal(respData, &entry)
	dlog.Printf("Unmarshaled '%s' into %#v\n", respData, entry)
	if err != nil {
		return proj, err
	}

	return entry.Data, nil
}

// UpdateTag changes information about an existing tag.
func (session *Session) UpdateTag(tag Tag) (Tag, error) {
	dlog.Printf("Updating tag %v", tag)
	data := map[string]interface{}{
		"tag": tag,
	}
	path := fmt.Sprintf("/tags/%v", tag.ID)
	respData, err := session.put(TogglAPI, path, data)

	if err != nil {
		return Tag{}, err
	}

	var entry struct {
		Data Tag `json:"data"`
	}
	err = json.Unmarshal(respData, &entry)
	dlog.Printf("Unmarshaled '%s' into %#v\n", data, entry)
	if err != nil {
		return Tag{}, err
	}

	return entry.Data, nil
}

// DeleteTag deletes a tag.
func (session *Session) DeleteTag(tag Tag) ([]byte, error) {
	dlog.Printf("Deleting tag %v", tag)
	path := fmt.Sprintf("/tags/%v", tag.ID)
	return session.delete(TogglAPI, path)
}

// GetClients returns a list of clients for the current account
func (session *Session) GetClients() (clients []Client, err error) {
	dlog.Println("Retrieving clients")

	data, err := session.get(TogglAPI, "/clients", nil)
	if err != nil {
		return clients, err
	}
	err = json.Unmarshal(data, &clients)
	return clients, err
}

// CreateClient adds a new client
func (session *Session) CreateClient(name string, wid int) (client Client, err error) {
	dlog.Printf("Creating client %s", name)
	data := map[string]interface{}{
		"client": map[string]interface{}{
			"name": name,
			"wid":  wid,
		},
	}

	respData, err := session.post(TogglAPI, "/clients", data)
	if err != nil {
		return client, err
	}

	var entry struct {
		Data Client `json:"data"`
	}
	err = json.Unmarshal(respData, &entry)
	dlog.Printf("Unmarshaled '%s' into %#v\n", respData, entry)
	if err != nil {
		return client, err
	}
	return entry.Data, nil
}

// Copy returns a copy of a TimeEntry.
func (e *TimeEntry) Copy() TimeEntry {
	newEntry := *e
	newEntry.Tags = make([]string, len(e.Tags))
	copy(newEntry.Tags, e.Tags)
	if e.Start != nil {
		newEntry.Start = &(*e.Start)
	}
	if e.Stop != nil {
		newEntry.Stop = &(*e.Stop)
	}
	return newEntry
}

// StartTime returns the start time of a time entry as a time.Time.
func (e *TimeEntry) StartTime() time.Time {
	if e.Start != nil {
		return *e.Start
	}
	return time.Time{}
}

// StopTime returns the stop time of a time entry as a time.Time.
func (e *TimeEntry) StopTime() time.Time {
	if e.Stop != nil {
		return *e.Stop
	}
	return time.Time{}
}

// HasTag returns true if a time entry contains a given tag.
func (e *TimeEntry) HasTag(tag string) bool {
	return indexOfTag(tag, e.Tags) != -1
}

// AddTag adds a tag to a time entry if the entry doesn't already contain the
// tag.
func (e *TimeEntry) AddTag(tag string) {
	if !e.HasTag(tag) {
		e.Tags = append(e.Tags, tag)
	}
}

// RemoveTag removes a tag from a time entry.
func (e *TimeEntry) RemoveTag(tag string) {
	if i := indexOfTag(tag, e.Tags); i != -1 {
		e.Tags = append(e.Tags[:i], e.Tags[i+1:]...)
	}
}

// SetDuration sets a time entry's duration. The duration should be a value in
// seconds. The stop time will also be updated. Note that the time entry must
// not be running.
func (e *TimeEntry) SetDuration(duration int64) error {
	if e.IsRunning() {
		return fmt.Errorf("TimeEntry must be stopped")
	}

	e.Duration = duration
	newStop := e.Start.Add(time.Duration(duration) * time.Second)
	e.Stop = &newStop

	return nil
}

// SetStartTime sets a time entry's start time. If the time entry is stopped,
// the stop time will also be updated.
func (e *TimeEntry) SetStartTime(start time.Time, updateEnd bool) {
	e.Start = &start

	if !e.IsRunning() {
		if updateEnd {
			newStop := start.Add(time.Duration(e.Duration) * time.Second)
			e.Stop = &newStop
		} else {
			e.Duration = e.Stop.Unix() - e.Start.Unix()
		}
	}
}

// SetStopTime sets a time entry's stop time. The duration will also be
// updated. Note that the time entry must not be running.
func (e *TimeEntry) SetStopTime(stop time.Time) (err error) {
	if e.IsRunning() {
		return fmt.Errorf("TimeEntry must be stopped")
	}

	e.Stop = &stop
	e.Duration = int64(stop.Sub(*e.Start) / time.Second)

	return nil
}

func indexOfTag(tag string, tags []string) int {
	for i, t := range tags {
		if t == tag {
			return i
		}
	}
	return -1
}

// UnmarshalJSON unmarshals a TimeEntry from JSON data, converting timestamp
// fields to Go Time values.
func (e *TimeEntry) UnmarshalJSON(b []byte) error {
	var entry tempTimeEntry
	err := json.Unmarshal(b, &entry)
	if err != nil {
		return err
	}
	te, err := entry.asTimeEntry()
	if err != nil {
		return err
	}
	*e = te
	return nil
}

// support /////////////////////////////////////////////////////////////

func (session *Session) request(method string, requestURL string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, requestURL, body)

	if session.APIToken != "" {
		req.SetBasicAuth(session.APIToken, "api_token")
	} else {
		req.SetBasicAuth(session.username, session.password)
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return content, fmt.Errorf(resp.Status)
	}

	return content, nil
}

func (session *Session) get(requestURL string, path string, params map[string]string) ([]byte, error) {
	requestURL += path

	if params != nil {
		data := url.Values{}
		for key, value := range params {
			data.Set(key, value)
		}
		requestURL += "?" + data.Encode()
	}

	dlog.Printf("GETing from URL: %s", requestURL)
	return session.request("GET", requestURL, nil)
}

func (session *Session) post(requestURL string, path string, data interface{}) ([]byte, error) {
	requestURL += path
	var body []byte
	var err error

	if data != nil {
		body, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
	}

	dlog.Printf("POSTing to URL: %s", requestURL)
	dlog.Printf("data: %s", body)
	return session.request("POST", requestURL, bytes.NewBuffer(body))
}

func (session *Session) put(requestURL string, path string, data interface{}) ([]byte, error) {
	requestURL += path
	var body []byte
	var err error

	if data != nil {
		body, err = json.Marshal(data)
		if err != nil {
			return nil, err
		}
	}

	dlog.Printf("PUTing to URL %s: %s", requestURL, string(body))
	return session.request("PUT", requestURL, bytes.NewBuffer(body))
}

func (session *Session) delete(requestURL string, path string) ([]byte, error) {
	requestURL += path
	dlog.Printf("DELETINGing URL: %s", requestURL)
	return session.request("DELETE", requestURL, nil)
}

func decodeSession(data []byte, session *Session) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(session)
	if err != nil {
		return err
	}
	return nil
}

func decodeAccount(data []byte, account *Account) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(account)
	if err != nil {
		return err
	}
	return nil
}

func decodeGroups(data []byte, groups *[]Group) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(groups)
	if err != nil {
		return err
	}
	return nil
}

func decodeSummaryReport(data []byte, report *SummaryReport) error {
	dlog.Printf("Decoding %s", data)
	dec := json.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(&report)
	if err != nil {
		return err
	}
	return nil
}

func decodeDetailedReport(data []byte, report *DetailedReport) error {
	dlog.Printf("Decoding %s", data)
	dec := json.NewDecoder(bytes.NewReader(data))
	err := dec.Decode(&report)
	if err != nil {
		return err
	}
	return nil
}

// This is an alias for TimeEntry that is used in tempTimeEntry to prevent the
// unmarshaler from infinitely recursing while unmarshaling.
type embeddedTimeEntry TimeEntry

// tempTimeEntry is an intermediate type used as for decoding TimeEntries.
type tempTimeEntry struct {
	embeddedTimeEntry
	Stop  string `json:"stop"`
	Start string `json:"start"`
}

func (t *tempTimeEntry) asTimeEntry() (entry TimeEntry, err error) {
	entry = TimeEntry(t.embeddedTimeEntry)

	parseTime := func(s string) (t time.Time, err error) {
		t, err = time.Parse("2006-01-02T15:04:05Z", s)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05-07:00", s)
		}
		return
	}

	if t.Start != "" {
		var start time.Time
		start, err = parseTime(t.Start)
		if err != nil {
			return
		}
		entry.Start = &start
	}

	if t.Stop != "" {
		var stop time.Time
		stop, err = parseTime(t.Stop)
		if err != nil {
			return
		}
		entry.Stop = &stop
	}

	return
}

func timeEntryRequest(data []byte, err error) (TimeEntry, error) {
	if err != nil {
		return TimeEntry{}, err
	}

	var entry struct {
		Data TimeEntry `json:"data"`
	}
	err = json.Unmarshal(data, &entry)
	dlog.Printf("Unmarshaled '%s' into %#v\n", data, entry)
	if err != nil {
		return TimeEntry{}, err
	}

	return entry.Data, nil
}

// DisableLog disables output to stderr
func DisableLog() {
	dlog.SetFlags(0)
	dlog.SetOutput(ioutil.Discard)
}

// EnableLog enables output to stderr
func EnableLog() {
	logFlags := dlog.Flags()
	dlog.SetFlags(logFlags)
	dlog.SetOutput(os.Stderr)
}
