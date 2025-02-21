package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/widget"
)

// getGitHubRepos fetches the authenticated user's repositories from GitHub.
func getGitHubRepos(user, token string) []string {
	client := &http.Client{}
	req, err := http.NewRequest("GET", "https://api.github.com/user/repos", nil)
	if err != nil {
		return nil
	}

	// Authenticate with GitHub PAT
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	body, _ := ioutil.ReadAll(resp.Body)

	// Parse JSON response
	var repos []struct {
		FullName string `json:"full_name"`
	}
	json.Unmarshal(body, &repos)

	// Extract repository names (format: owner/repo)
	var repoList []string
	for _, repo := range repos {
		repoList = append(repoList, repo.FullName)
	}

	return repoList
}

// createAzureRepo creates a new repository in Azure DevOps.
func createAzureRepo(repoName, org, project, token string) (string, error) {
	// Construct URL. org should be the URL of your Azure DevOps organization.
	url := fmt.Sprintf("%s/%s/_apis/git/repositories?api-version=7.0", org, project)

	// Create JSON payload
	payload := map[string]interface{}{
		"name": repoName,
	}
	jsonPayload, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}

	// Authenticate with Azure PAT (using empty username)
	req.SetBasicAuth("", token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("Azure API error: %s", resp.Status)
	}

	// Parse response to get repository URL
	var result struct {
		RemoteUrl string `json:"remoteUrl"`
	}
	body, _ := ioutil.ReadAll(resp.Body)
	json.Unmarshal(body, &result)

	// Insert PAT into URL for authentication (if desired)
	remoteURL := strings.Replace(result.RemoteUrl, "dev.azure.com", fmt.Sprintf("%s@dev.azure.com", token), 1)

	return remoteURL, nil
}

func main() {
	// Create the Fyne app and window.
	a := app.New()
	w := a.NewWindow("GitHub to Azure Migration")
	w.Resize(fyne.NewSize(600, 500))

	// Create a binding for the logs.
	logBinding := binding.NewString()
	logEntry := widget.NewMultiLineEntry()
	logEntry.Bind(logBinding)
	logEntry.SetPlaceHolder("Logs will appear here...")
	logEntry.Wrapping = fyne.TextWrapWord
	logEntry.Disable() // make read-only

	// Helper function to append log messages.
	appendLog := func(msg string) {
		// Prepend timestamp
		timestamp := time.Now().Format("15:04:05")
		newLog := fmt.Sprintf("[%s] %s\n", timestamp, msg)
		current, _ := logBinding.Get()
		// Update binding (thread-safe)
		logBinding.Set(current + newLog)
	}

	// Create input fields for GitHub and Azure details.
	githubTokenEntry := widget.NewEntry()
	githubTokenEntry.SetPlaceHolder("GitHub PAT Token")

	azureTokenEntry := widget.NewEntry()
	azureTokenEntry.SetPlaceHolder("Azure DevOps PAT Token")

	azureOrgEntry := widget.NewEntry()
	azureOrgEntry.SetPlaceHolder("Azure Organization URL (e.g. https://dev.azure.com/yourOrg)")

	azureProjectEntry := widget.NewEntry()
	azureProjectEntry.SetPlaceHolder("Azure Project Name")

	// Checkbox for "Don't save local clone"
	dontSaveCheckbox := widget.NewCheck("Don't save local clone", nil)

	// Migrate button
	migrateBtn := widget.NewButton("Migrate", func() {
		// Run the migration in a separate goroutine so the UI remains responsive.
		go func() {
			appendLog("Starting migration...")

			githubToken := strings.TrimSpace(githubTokenEntry.Text)
			azureToken := strings.TrimSpace(azureTokenEntry.Text)
			azureOrg := strings.TrimSpace(azureOrgEntry.Text)
			azureProject := strings.TrimSpace(azureProjectEntry.Text)

			if githubToken == "" || azureToken == "" || azureOrg == "" || azureProject == "" {
				appendLog("Error: All fields are required.")
				return
			}

			// Fetch GitHub repositories.
			appendLog("Fetching repositories from GitHub...")
			repos := getGitHubRepos("", githubToken)
			if repos == nil || len(repos) == 0 {
				appendLog("No repositories found or error occurred while fetching repos.")
				return
			}
			appendLog(fmt.Sprintf("Found %d repositories.", len(repos)))

			// Process each repository.
			for _, repo := range repos {
				appendLog(fmt.Sprintf("Migrating repository: %s", repo))
				// Create new repo in Azure DevOps.
				azureRepoURL, err := createAzureRepo(repo, azureOrg, azureProject, azureToken)
				if err != nil {
					appendLog(fmt.Sprintf("Error creating Azure repo for %s: %v", repo, err))
					continue
				}
				appendLog(fmt.Sprintf("Created Azure repo: %s", azureRepoURL))

				// Construct GitHub repo URL with token for authentication.
				// Note: Including the token in the URL can be a security risk in production.
				githubRepoURL := fmt.Sprintf("https://%s@github.com/%s.git", githubToken, repo)

				// Create a temporary directory for the bare clone.
				tempDir, err := ioutil.TempDir("", strings.ReplaceAll(repo, "/", "_"))
				if err != nil {
					appendLog(fmt.Sprintf("Error creating temporary directory for %s: %v", repo, err))
					continue
				}
				appendLog(fmt.Sprintf("Cloning repository into %s", tempDir))

				// Clone the repository as a bare clone.
				cloneCmd := exec.Command("git", "clone", "--bare", githubRepoURL, tempDir)
				if output, err := cloneCmd.CombinedOutput(); err != nil {
					appendLog(fmt.Sprintf("Error cloning %s: %v, output: %s", repo, err, string(output)))
					// Clean up tempDir if clone fails.
					os.RemoveAll(tempDir)
					continue
				}

				// Add Azure remote.
				remoteAddCmd := exec.Command("git", "-C", tempDir, "remote", "add", "azure", azureRepoURL)
				if output, err := remoteAddCmd.CombinedOutput(); err != nil {
					appendLog(fmt.Sprintf("Error adding Azure remote for %s: %v, output: %s", repo, err, string(output)))
					os.RemoveAll(tempDir)
					continue
				}

				// Push all branches.
				pushAllCmd := exec.Command("git", "-C", tempDir, "push", "azure", "--all")
				if output, err := pushAllCmd.CombinedOutput(); err != nil {
					appendLog(fmt.Sprintf("Error pushing branches for %s: %v, output: %s", repo, err, string(output)))
					os.RemoveAll(tempDir)
					continue
				}

				// Push tags.
				pushTagsCmd := exec.Command("git", "-C", tempDir, "push", "azure", "--tags")
				if output, err := pushTagsCmd.CombinedOutput(); err != nil {
					appendLog(fmt.Sprintf("Error pushing tags for %s: %v, output: %s", repo, err, string(output)))
					os.RemoveAll(tempDir)
					continue
				}

				appendLog(fmt.Sprintf("Successfully migrated %s to Azure.", repo))

				// If "Don't save local clone" is checked, remove the temporary clone.
				if dontSaveCheckbox.Checked {
					err = os.RemoveAll(tempDir)
					if err != nil {
						appendLog(fmt.Sprintf("Error removing local clone for %s: %v", repo, err))
					} else {
						appendLog(fmt.Sprintf("Removed local clone for %s.", repo))
					}
				} else {
					// Otherwise, move the clone to a designated folder.
					destDir := filepath.Join(".", "clones", strings.ReplaceAll(repo, "/", "_"))
					err = os.MkdirAll(filepath.Dir(destDir), 0755)
					if err == nil {
						err = os.Rename(tempDir, destDir)
					}
					if err != nil {
						appendLog(fmt.Sprintf("Error moving clone for %s to %s: %v", repo, destDir, err))
					} else {
						appendLog(fmt.Sprintf("Local clone for %s saved to %s.", repo, destDir))
					}
				}
			}

			appendLog("Migration completed.")
		}()
	})

	// Layout the UI.
	form := container.NewVBox(
		widget.NewLabel("GitHub to Azure Migration Tool"),
		widget.NewForm(
			widget.NewFormItem("GitHub PAT", githubTokenEntry),
			widget.NewFormItem("Azure PAT", azureTokenEntry),
			widget.NewFormItem("Azure Org URL", azureOrgEntry),
			widget.NewFormItem("Azure Project", azureProjectEntry),
		),
		dontSaveCheckbox,
		migrateBtn,
		widget.NewLabel("Logs:"),
		logEntry,
	)

	// Set the content and show the window.
	w.SetContent(form)
	w.ShowAndRun()
}
