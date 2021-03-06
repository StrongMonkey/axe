package k8s

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gdamore/tcell"
	"github.com/rancher/axe/throwing"
	"github.com/rancher/axe/throwing/datafeeder"
	"github.com/rancher/axe/throwing/types"
	"github.com/rancher/norman/pkg/kv"
	"github.com/rivo/tview"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getNamespaceAndName(t *throwing.TableView) (string, string) {
	table := t.GetTable()
	namespaced := false
	if strings.Contains(table.GetCell(0, 0).Text, "NAMESPACE") {
		namespaced = true
	}

	row, _ := table.GetSelection()
	var namespace, name string
	if namespaced {
		namespace, name = table.GetCell(row, 0).Text, table.GetCell(row, 1).Text
	} else {
		name = table.GetCell(row, 0).Text
	}
	return namespace, name
}

func get(t *throwing.TableView) {
	out := &strings.Builder{}
	errB := &strings.Builder{}

	var args []string
	namespace, name := getNamespaceAndName(t)
	if namespace != "" {
		args = []string{"get", t.GetResourceKind(), "-n", namespace, name, "-o", "yaml"}
	} else {
		args = []string{"get", t.GetResourceKind(), name, "-o", "yaml"}
	}
	cmd := exec.Command("kubectl", args...)
	cmd.Stdout, cmd.Stderr = out, errB
	if err := cmd.Run(); err != nil {
		t.UpdateStatus(errB.String(), true)
		return
	}

	box := tview.NewTextView()
	box.SetDynamicColors(true).SetBackgroundColor(tcell.ColorBlack)
	box.SetText(out.String())
	box.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEscape {
			t.SwitchToRootPage()
		}
	})

	newpage := tview.NewPages().AddPage("get", box, true, true)
	t.SwitchPage(t.GetCurrentPage(), newpage)
}

func edit(t *throwing.TableView) {
	namespace, name := getNamespaceAndName(t)
	errb := &strings.Builder{}
	var args []string
	if namespace != "" {
		args = []string{"edit", t.GetResourceKind(), "-n", namespace, name}
	} else {
		args = []string{"edit", t.GetResourceKind(), name}
	}
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, errb

	t.GetApplication().Suspend(func() {
		clearScreen()
		if err := cmd.Run(); err != nil {
			t.UpdateStatus(errb.String(), true)
		}
		return
	})
}

func execute(t *throwing.TableView) {
	if t.GetResourceKind() != "pods" {
		return
	}

	namespace, name := getNamespaceAndName(t)
	errb := &strings.Builder{}
	shellArgs := []string{"/bin/sh", "-c", "TERM=xterm-256color; export TERM; [ -x /bin/bash ] && ([ -x /usr/bin/script ] && /usr/bin/script -q -c /bin/bash /dev/null || exec /bin/bash) || exec /bin/sh"}
	args := append([]string{"exec", "-it", "-n", namespace, name, "--"}, shellArgs...)
	cmd := exec.Command("kubectl", args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, errb

	t.GetApplication().Suspend(func() {
		clearScreen()
		if err := cmd.Run(); err != nil {
			t.UpdateStatus(errb.String(), true)
		}
		return
	})
}

func logs(t *throwing.TableView) {
	if t.GetResourceKind() != "pods" {
		return
	}

	errB := &strings.Builder{}
	var args []string
	namespace, name := getNamespaceAndName(t)
	args = []string{"logs", "-f", "-n", namespace, name, "--all-containers"}
	cmd := exec.Command("kubectl", args...)
	cmd.Stderr = errB

	logbox := tview.NewTextView()
	{
		logbox.SetTitle(fmt.Sprintf("logs - (%s)", name))
		logbox.SetBorder(true)
		logbox.SetTitleColor(tcell.ColorPurple)
		logbox.SetDynamicColors(true)
		logbox.SetBackgroundColor(tcell.ColorBlack)
		logbox.SetChangedFunc(func() {
			logbox.ScrollToEnd()
			t.GetApplication().Draw()
		})
		logbox.SetDoneFunc(func(key tcell.Key) {
			if key == tcell.KeyEscape {
				cmd.Process.Kill()
			}
		})
	}

	cmd.Stdout = tview.ANSIWriter(logbox)
	go func() {
		if err := cmd.Run(); err != nil {
			return
		}
	}()

	newpage := tview.NewPages().AddPage("logs", logbox, true, true)
	t.SwitchPage(t.GetCurrentPage(), newpage)
}

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

func delete(t *throwing.TableView) {
	namespace, name := getNamespaceAndName(t)
	modal := tview.NewModal().
		SetText(fmt.Sprintf("Do you want to delete %s %s?", t.GetResourceKind(), name)).
		AddButtons([]string{"delete", "Cancel"}).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			if buttonLabel == "delete" {
				var args []string
				if namespace != "" {
					args = []string{"delete", t.GetResourceKind(), "-n", namespace, name}
				} else {
					args = []string{"delete", t.GetResourceKind(), name}
				}
				cmd := exec.Command("kubectl", args...)
				errB := &strings.Builder{}
				cmd.Stderr = errB
				go func() {
					if err := cmd.Run(); err != nil {
						t.UpdateStatus(errB.String(), true)
						return
					}
				}()
				t.Refresh()
				t.SwitchToRootPage()
			} else if buttonLabel == "Cancel" {
				t.BackPage()
			}
		})
	t.InsertDialog("delete", t.GetCurrentPrimitive(), modal)
}

func resourceView(t *throwing.TableView) error {
	viewResource(t)
	return nil
}

func viewResource(t *throwing.TableView) {
	table := t.GetTable()
	row, _ := table.GetSelection()
	kind := table.GetCell(row, 0).Text
	groupVersion := table.GetCell(row, 1).Text

	var apiResource metav1.APIResource
	group, version := kv.Split(groupVersion, "/")
	if version == "" {
		version = group
		group = ""
	}
	apiResource.Group = group
	apiResource.Version = version
	apiResource.Name = kind

	withGroup := kind
	if group != "" {
		withGroup += "." + group
	}
	rkind := types.ResourceKind{
		Title: withGroup,
		Kind:  withGroup,
	}

	w := wrapper{
		group:   apiResource.Group,
		version: apiResource.Version,
		name:    apiResource.Name,
	}

	feeder := datafeeder.NewDataFeeder(w.refreshResource)

	newtable := t.GetNestedTable(rkind.Kind)
	if newtable == nil {
		newtable = t.NewNestTableView(rkind, feeder, nil, nil, itemEventHandler)
	}
	t.SetTableView(rkind.Kind, newtable)

	t.SwitchPage(rkind.Kind, newtable)
}
