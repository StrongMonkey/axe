package axe

import (
	"context"
	"fmt"
	"sync"

	"github.com/gdamore/tcell"
	"github.com/rancher/axe/version"
	"github.com/rivo/tview"
	"k8s.io/client-go/kubernetes"
)

var logo = ` 
    _              
   / \   __  _____ 
  / _ \  \ \/ / _ \
 / ___ \  >  <  __/
/_/   \_\/_/\_\___|
`

type AppView struct {
	*tview.Flex
	*tview.Application
	context     context.Context
	cancel      context.CancelFunc
	version     string
	k8sVersion  string
	clientset   *kubernetes.Clientset
	menuView    menuView
	footerView  footerView
	statusView  statusView
	content     contentView
	drawQueue   *PrimitiveQueue
	tableViews  map[string]*tableView
	pageRows    map[string]position
	showMenu    bool
	currentPage string
	switchPage  chan struct{}
	syncs       map[string]chan struct{}
	lock        sync.Mutex
}

type position struct {
	row, column int
}

func NewAppView(clientset *kubernetes.Clientset) *AppView {
	v := &AppView{Application: tview.NewApplication()}
	{
		v.Flex = tview.NewFlex()
		v.drawQueue = &PrimitiveQueue{AppView: v}
		v.menuView = menuView{AppView: v, Flex: tview.NewFlex()}
		v.content = contentView{AppView: v, Pages: tview.NewPages()}
		v.footerView = footerView{AppView: v, TextView: tview.NewTextView()}
		v.statusView = statusView{AppView: v, TextView: tview.NewTextView()}
		v.pageRows = make(map[string]position)
		v.clientset = clientset

		{
			v.menuView.SetBackgroundColor(tcell.ColorBlack)
			v.content.Pages.SetBackgroundColor(tcell.ColorBlack)
			v.footerView.SetBackgroundColor(tcell.ColorDarkCyan)
		}
	}
	return v
}

func (app *AppView) Init() error {
	app.version = version.VERSION
	k8sversion, err := app.getK8sVersion()
	if err != nil {
		return err
	}
	app.context, app.cancel = context.WithCancel(context.Background())
	app.k8sVersion = k8sversion
	app.menuView.init()
	app.footerView.init()
	app.statusView.init()
	app.content.init()
	app.switchPage = make(chan struct{}, 1)
	app.tableViews = map[string]*tableView{
		RootPage: NewTableView(app, RootPage, tableEventHandler),
	}

	// set default page to root page
	app.footerView.TextView.Highlight(RootPage).ScrollToHighlight()
	app.content.SwitchPage(RootPage, app.tableViews[RootPage])

	app.setInputHandler()

	go app.watch()

	main := tview.NewFlex()
	{
		main.SetDirection(tview.FlexRow)
		main.AddItem(app.content, 0, 15, true)

		footer := tview.NewFlex()
		footer.AddItem(app.footerView, 0, 1, false)
		footer.AddItem(app.statusView, 0, 1, false)

		main.AddItem(footer, 1, 1, false)
	}

	app.Application.SetRoot(main, true)
	return nil
}

func (app *AppView) watch() {
	for {
		select {
		case <-app.switchPage:
			app.cancel()
			app.tableViews[app.currentPage].run(app.context)
		}
	}
}

/*
setInputHandler setup the input event handler for main page

PageNav: Navigate different pages listed in footer
M(Menu): Menu view
Escape: go back to the previous view
*/
func (app *AppView) setInputHandler() {
	app.Application.SetInputCapture(RootEventHandler(app))
}

func (app *AppView) menuDecor(page string, p tview.Primitive) {
	newpage := tview.NewPages()
	newpage.AddPage(page, p, true, true).AddPage("menu", center(app.menuView, 30, 20), true, true)
	app.SwitchPage(page, newpage)
}

func (app *AppView) getK8sVersion() (string, error) {
	ver, err := app.clientset.Discovery().ServerVersion()
	if err != nil {
		return "", err
	}
	return ver.GitVersion, nil
}

func (app *AppView) SwitchPage(page string, p tview.Primitive) {
	app.lock.Lock()
	if app.currentPage != page {
		app.currentPage = page
		app.switchPage <- struct{}{}
	}
	app.lock.Unlock()
	app.content.AddAndSwitchToPage(page, p, true)
	app.drawQueue.Enqueue(PageTrack{
		PageName:  page,
		Primitive: p,
	})
	app.SetFocus(p)
}

func (app *AppView) CurrentPage() tview.Primitive {
	return app.tableViews[app.currentPage]
}

func (app *AppView) LastPage() {
	app.drawQueue.Dequeue()
	page := app.drawQueue.Last()
	app.content.AddAndSwitchToPage(page.PageName, page.Primitive, true)
	app.content.SetFocus(page.Primitive)
}

type menuView struct {
	*tview.Flex
	*AppView
}

func (m *menuView) init() {
	{
		m.Flex.SetDirection(tview.FlexRow)
		m.Flex.SetBackgroundColor(tcell.ColorGray)
		m.Flex.AddItem(m.logoView(), 6, 1, false)
		m.Flex.AddItem(m.versionView(), 4, 1, false)
		m.Flex.AddItem(m.tipsView(), 12, 1, false)
	}
}

func (m *menuView) logoView() *tview.TextView {
	t := tview.NewTextView()
	t.SetBackgroundColor(tcell.ColorGray)
	t.SetText(logo).SetTextColor(tcell.ColorBlack).SetTextAlign(tview.AlignCenter).SetBorderAttributes(tcell.AttrBold)
	return t
}

func (m *menuView) versionView() *tview.Table {
	t := tview.NewTable()
	t.SetBackgroundColor(tcell.ColorGray)
	t.SetBorder(true)
	t.SetTitle("Version")
	rioVersionHeader := tview.NewTableCell("Axe Version:").SetAlign(tview.AlignCenter).SetExpansion(2)
	rioVersionValue := tview.NewTableCell(m.version).SetTextColor(tcell.ColorPurple).SetAlign(tview.AlignCenter).SetExpansion(2)

	k8sVersionHeader := tview.NewTableCell("K8s Version:").SetAlign(tview.AlignCenter).SetExpansion(2)
	k8sVersionValue := tview.NewTableCell(m.k8sVersion).SetTextColor(tcell.ColorPurple).SetAlign(tview.AlignCenter).SetExpansion(2)

	t.SetCell(0, 0, rioVersionHeader)
	t.SetCell(0, 1, rioVersionValue)
	t.SetCell(1, 0, k8sVersionHeader)
	t.SetCell(1, 1, k8sVersionValue)
	return t
}

func (m *menuView) tipsView() *tview.Table {
	t := tview.NewTable()
	t.SetBorderPadding(1, 0, 0, 0)
	t.SetBackgroundColor(tcell.ColorGray)
	t.SetBorder(true)
	t.SetTitle("Shortcuts")
	var row int
	for _, values := range Shortcuts {
		kc, vc := newKeyValueCell(values[0], values[1])
		t.SetCell(row, 0, kc)
		t.SetCell(row, 1, vc)
		row++
	}
	return t
}

func newKeyValueCell(key, value string) (*tview.TableCell, *tview.TableCell) {
	keycell := tview.NewTableCell(key).SetAlign(tview.AlignCenter).SetExpansion(2)
	valuecell := tview.NewTableCell(value).SetTextColor(tcell.ColorPurple).SetAlign(tview.AlignCenter).SetExpansion(2)
	return keycell, valuecell
}

type statusView struct {
	*tview.TextView
	*AppView
}

func (s *statusView) init() {
	s.TextView.SetBackgroundColor(tcell.ColorGray)
}

type footerView struct {
	*tview.TextView
	*AppView
}

func (f *footerView) init() {
	f.TextView.
		SetDynamicColors(true).
		SetRegions(true).
		SetWrap(false).SetBackgroundColor(tcell.ColorGray)
	for index, t := range Footers {
		fmt.Fprintf(f.TextView, `%d ["%s"][black]%s[white][""] `, index+1, t.Kind, t.Title)
	}
}

type contentView struct {
	*tview.Pages
	*AppView
}

func (c *contentView) init() {}

var center = func(p tview.Primitive, width, height int) tview.Primitive {
	newflex := tview.NewFlex()
	newflex.
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, true).
			AddItem(nil, 0, 1, false), width, 1, false).
		AddItem(nil, 0, 1, false)
	return newflex
}
