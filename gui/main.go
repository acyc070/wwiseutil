package main

import (
	"log"
	"os"
)

import (
	"gui/viewer"
	"github.com/therecipe/qt/core"
	"github.com/therecipe/qt/widgets"
)

const (
	windowWidth  = 860
	windowHeight = 480
)

func main() {
	log.Println("Starting wwiseutil GUI...")
	app := widgets.NewQApplication(len(os.Args), os.Args)
	core.QCoreApplication_SetApplicationName("Wwise Audio Utilities")
	core.QCoreApplication_SetApplicationVersion("1.0")

	parser := core.NewQCommandLineParser()
	parser.SetApplicationDescription(core.QCoreApplication_ApplicationName())
	parser.AddHelpOption()
	parser.AddVersionOption()
	parser.Process2(app)

	window := viewer.New()

	availableGeometry := widgets.QApplication_Desktop().AvailableGeometry2(window)
	window.Resize2(windowWidth, windowHeight)
	// Move the window to the center of the screen.
	window.Move2((availableGeometry.Width()-window.Width())/2,
		(availableGeometry.Height()-window.Height())/2)

	window.Show()
	app.Exec()
}
