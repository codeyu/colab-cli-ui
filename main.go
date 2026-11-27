// Colab CLI UI is a desktop client for google-colab-cli: it creates and stops
// Colab runtimes, runs code on them, browses their files and shows the
// account's compute units, by driving the `colab` command.
package main

import (
	"log"

	"github.com/egoist/mygo"
)

func main() {
	app := mygo.App
	// Set the name first: it determines the user data directory.
	app.SetName("Colab CLI UI")

	mygo.Bind(&Colab{})

	app.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Colab CLI UI",
			Width:           1280,
			Height:          820,
			MinWidth:        900,
			MinHeight:       560,
			BackgroundColor: "light-dark(#ffffff, #0d0d0f)", // the page's --background
			StateKey:        "main",
			URL:             "/",
		})
		setupDesktop(win)
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
