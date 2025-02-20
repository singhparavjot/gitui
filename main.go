package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func migrateRepo(gitHubOrg, adoOrg, adoProject, repoName, gitPat, adoPat string, deleteAfter bool, logBox *widget.Label) {
	logMsg := func(msg string) {
		logBox.SetText(logBox.Text + "\n" + msg)
	}

	logMsg(fmt.Sprintf("Migrating repository: %s", repoName))

	// Clone the GitHub repository locally
	cmd := exec.Command("git", "clone", "--mirror", fmt.Sprintf("https://%s@github.com/%s/%s.git", gitPat, gitHubOrg, repoName))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		logMsg(fmt.Sprintf("Failed to clone repository: %s", err))
		return
	}

	dirName := fmt.Sprintf("%s.git", repoName)
	cmd = exec.Command("git", "-C", dirName, "remote", "add", "azure-devops", fmt.Sprintf("https://%s@dev.azure.com/%s/%s/_git/%s", adoPat, adoOrg, adoProject, repoName))
	err = cmd.Run()
	if err != nil {
		logMsg(fmt.Sprintf("Failed to add Azure DevOps remote: %s", err))
		return
	}

	cmd = exec.Command("git", "-C", dirName, "push", "--mirror", "azure-devops")
	err = cmd.Run()
	if err != nil {
		logMsg(fmt.Sprintf("Failed to push repository: %s", err))
		return
	}

	if deleteAfter {
		err = os.RemoveAll(dirName)
		if err != nil {
			logMsg(fmt.Sprintf("Failed to delete repository: %s", err))
			return
		}
		logMsg("Successfully migrated and deleted local repository: " + repoName)
	} else {
		logMsg("Successfully migrated repository: " + repoName)
	}
}

func main() {
	myApp := app.New()
	myWindow := myApp.NewWindow("GitHub to ADO Migrator")
	myWindow.Resize(fyne.NewSize(600, 400))

	logBox := widget.NewLabel("Logs:")

	gitHubOrg := widget.NewEntry()
	adoOrg := widget.NewEntry()
	adoProject := widget.NewEntry()
	repoList := widget.NewEntry()
	gitPat := widget.NewPasswordEntry()
	adoPat := widget.NewPasswordEntry()
	deleteAfter := widget.NewCheck("Don't Save (Delete after Migration)", nil)

	migrateButton := widget.NewButton("Migrate", func() {
		repos := strings.Split(repoList.Text, ",")
		for _, repo := range repos {
			go migrateRepo(strings.TrimSpace(gitHubOrg.Text), strings.TrimSpace(adoOrg.Text), strings.TrimSpace(adoProject.Text), strings.TrimSpace(repo), strings.TrimSpace(gitPat.Text), strings.TrimSpace(adoPat.Text), deleteAfter.Checked, logBox)
		}
	})

	form := container.NewVBox(
		widget.NewLabel("GitHub Org"), gitHubOrg,
		widget.NewLabel("ADO Org"), adoOrg,
		widget.NewLabel("ADO Project"), adoProject,
		widget.NewLabel("Repo Names (comma-separated)"), repoList,
		widget.NewLabel("GitHub PAT"), gitPat,
		widget.NewLabel("ADO PAT"), adoPat,
		deleteAfter,
		migrateButton,
		logBox,
	)

	myWindow.SetContent(form)
	myWindow.SetCloseIntercept(func() {
		myApp.Quit()
	})
	myWindow.ShowAndRun()
}
