package ui

type ColorProfile struct {
	primaryColor           string
	secondaryColor         string
	invertedPrimaryColor   string
	invertedSecondaryColor string
}

type Theme struct {
	Name  string
	Dark  ColorProfile
	Light ColorProfile
}

var Themes = []Theme{
	{
		Name:  "Grayscale",
		Dark:  ColorProfile{primaryColor: "#e5e5e5ff", secondaryColor: "#bdbdbdff", invertedPrimaryColor: "#2e2e2eff", invertedSecondaryColor: "#818181ff"},
		Light: ColorProfile{primaryColor: "#2e2e2eff", secondaryColor: "#818181ff", invertedPrimaryColor: "#e5e5e5ff", invertedSecondaryColor: "#bdbdbdff"},
	},
	{
		Name:  "Catppuccin Macchiato",
		Dark:  ColorProfile{primaryColor: "#f5bde6", secondaryColor: "#a6da95", invertedPrimaryColor: "#24273a", invertedSecondaryColor: "#494d64"},
		Light: ColorProfile{primaryColor: "#ec7486", secondaryColor: "#8ccf7f", invertedPrimaryColor: "#cad3f5", invertedSecondaryColor: "#b8c0e0"},
	},
	{
		Name:  "Nord",
		Dark:  ColorProfile{primaryColor: "#88C0D0", secondaryColor: "#A3BE8C", invertedPrimaryColor: "#2E3440", invertedSecondaryColor: "#4C566A"},
		Light: ColorProfile{primaryColor: "#5E81AC", secondaryColor: "#BF616A", invertedPrimaryColor: "#ECEFF4", invertedSecondaryColor: "#D8DEE9"},
	},
	{
		Name:  "Tokyo Night",
		Dark:  ColorProfile{primaryColor: "#bb9af7", secondaryColor: "#e0af68", invertedPrimaryColor: "#1a1b26", invertedSecondaryColor: "#15161e"},
		Light: ColorProfile{primaryColor: "#7aa2f7", secondaryColor: "#f7768e", invertedPrimaryColor: "#c0caf5", invertedSecondaryColor: "#a9b1d6"},
	},
	{
		Name:  "Solarized",
		Dark:  ColorProfile{primaryColor: "#2AA198", secondaryColor: "#B58900", invertedPrimaryColor: "#002B36", invertedSecondaryColor: "#073642"},
		Light: ColorProfile{primaryColor: "#2AA198", secondaryColor: "#B58900", invertedPrimaryColor: "#FDF6E3", invertedSecondaryColor: "#EEE8D5"},
	},
	{
		Name:  "Monokai",
		Dark:  ColorProfile{primaryColor: "#F92672", secondaryColor: "#A6E22E", invertedPrimaryColor: "#272822", invertedSecondaryColor: "#75715E"},
		Light: ColorProfile{primaryColor: "#F92672", secondaryColor: "#A6E22E", invertedPrimaryColor: "#F9F8F5", invertedSecondaryColor: "#E6DB74"},
	},
	{
		Name:  "Nordfox",
		Dark:  ColorProfile{primaryColor: "#88c0d0", secondaryColor: "#bf616a", invertedPrimaryColor: "#2e3440", invertedSecondaryColor: "#3b4252"},
		Light: ColorProfile{primaryColor: "#81a1c1", secondaryColor: "#a3be8c", invertedPrimaryColor: "#e5e9f0", invertedSecondaryColor: "#e7ecf4"},
	},
	{
		Name:  "Royal",
		Dark:  ColorProfile{primaryColor: "#b49d27", secondaryColor: "#91284c", invertedPrimaryColor: "#100815", invertedSecondaryColor: "#241f2b"},
		Light: ColorProfile{primaryColor: "#6580b0", secondaryColor: "#674d96", invertedPrimaryColor: "#9e8cbd", invertedSecondaryColor: "#524966"},
	},
	{
		Name:  "Matrix",
		Dark:  ColorProfile{primaryColor: "#00ff00", secondaryColor: "#008800", invertedPrimaryColor: "#000000", invertedSecondaryColor: "#003300"},
		Light: ColorProfile{primaryColor: "#00aa00", secondaryColor: "#005500", invertedPrimaryColor: "#ffffff", invertedSecondaryColor: "#e0e0e0"},
	},
	{
		Name:  "Ghostty Default Style Dark",
		Dark:  ColorProfile{primaryColor: "#0f0f11", secondaryColor: "#2c42c1", invertedPrimaryColor: "#f0f0ee", invertedSecondaryColor: "#374151"},
		Light: ColorProfile{primaryColor: "#000000", secondaryColor: "#666666", invertedPrimaryColor: "#ffffff", invertedSecondaryColor: "#e5e5e5"},
	},
}
