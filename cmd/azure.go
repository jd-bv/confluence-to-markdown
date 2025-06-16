/*
Copyright © 2023 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/jd-bv/confluence-to-markdown/confluence"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var baseUrl string
var baseUrlWiki string
var attachmentsFolder string

type promptContent struct {
	errorMsg string
	label    string
	mask     rune
}

func promptGetInput(pc promptContent) string {
	validate := func(input string) error {
		if len(input) <= 0 {
			return errors.New(pc.errorMsg)
		}
		return nil
	}

	templates := &promptui.PromptTemplates{
		Prompt:  "{{ . }} ",
		Valid:   "{{ . | green }} ",
		Invalid: "{{ . | red }} ",
		Success: "{{ . | bold }} ",
	}

	prompt := promptui.Prompt{
		Label:     pc.label,
		Templates: templates,
		Validate:  validate,
		Mask:      pc.mask,
	}

	result, err := prompt.Run()
	if err != nil {
		fmt.Printf("Prompt failed %v\n", err)
		os.Exit(1)
	}

	return result
}

// azureCmd represents the azure command
var azureCmd = &cobra.Command{
	Use:        "azure <space>",
	Short:      "converts confluence space to azure wiki format",
	Args:       cobra.ExactArgs(1),
	ArgAliases: []string{"space"},
	Long: `Convert a confluence space to azure wiki format.

All pages and attachments will be downloaded and converted to markdown.

Example:
confluence-to-markdown azure --baseUrl https://mycompany.atlassian.net --user myuser@mycompany.com --token my-secret-confluence-token myspace
`,
	Run: func(cmd *cobra.Command, args []string) {
		// get the url from args
		space := args[0]

		baseUrl, err := cmd.Flags().GetString("baseUrl")
		baseUrlWiki = fmt.Sprintf("%s/wiki", baseUrl)
		cobra.CheckErr(err)
		_, err = url.ParseRequestURI(baseUrlWiki)
		cobra.CheckErr(err)

		// token, err := cmd.Flags().GetString("token")
		// cobra.CheckErr(err)

		// login details
		if len(username) < 1 {
			username = promptGetInput(promptContent{
				label:    "Username",
				errorMsg: "Invalid username",
			})
		}

		// Create root output folder
		if err := os.Mkdir(space, 0755); err != nil {
			log.Fatal(err)
		}

		attachmentsFolder = fmt.Sprintf("%s/.attachments", space)
		// attachments
		if err := os.Mkdir(attachmentsFolder, 0755); err != nil {
			log.Fatal(err)
		}

		// get base from config
		// get space name from input

		client := &http.Client{}

		request, err := http.NewRequest("GET", fmt.Sprintf("%s/rest/api/space/%s", baseUrlWiki, space), nil)
		cobra.CheckErr(err)
		request.Header.Set("Content-Type", "application/json")
		request.SetBasicAuth(username, token)

		response, err := client.Do(request)
		cobra.CheckErr(err)

		body, err := io.ReadAll(response.Body)
		cobra.CheckErr(err)

		var data confluence.Space
		if err = json.Unmarshal(body, &data); err != nil {
			cobra.CheckErr(err)
		}

		handlePage(fmt.Sprintf("%s%s", baseUrlWiki, data.Expandable.Homepage), space)
	},
}

func hasChildren(page confluence.Page) bool {
	return page.Children != nil && page.Children.Page != nil && page.Children.Page.Results != nil && len(page.Children.Page.Results) > 0
}

// Bunch of rules for names in wiki.
// https://learn.microsoft.com/en-gb/azure/devops/project/wiki/wiki-file-structure?view=azure-devops#file-naming-conventions
var azureReplacements = []struct{ old, new string }{
	{"/", "__"},
	{":", "%3A"},
	{"<", "%3C"},
	{">", "%3E"},
	{"*", "%2A"},
	{"?", "%3F"},
	{"|", "%7C"},
	{"-", "%2D"},
	{"\"", "%22"},
	{" ", "-"},
}

func makeAzureWikiFriendlyTitle(s string) string {
	for _, r := range azureReplacements {
		s = strings.ReplaceAll(s, r.old, r.new)
	}

	return s
}

func handlePage(u string, outputPath string) {
	// get page
	page, err := fetchPage(u)
	if err != nil {
		log.Fatal(err)
	}

	if hasChildren(page) {
		// create subfolder
		if err := os.MkdirAll(fmt.Sprintf("%s/%s", outputPath, makeAzureWikiFriendlyTitle(page.Title)), 0755); err != nil {
			log.Fatal(err)
		}
		outputPath = fmt.Sprintf("%s/%s", outputPath, makeAzureWikiFriendlyTitle(page.Title))
	}

	// Convert to md
	converter := md.NewConverter("", true, nil)
	converted, err := converter.ConvertString(page.Body.ExportView.Value)
	if err != nil {
		log.Fatal(err)
	}

	// find all attachments, and download them to local folder
	converted, err = downloadAttachmentsToLocalAndReplaceLinks(converted, page)

	// write to file
	parentPath := strings.Join(strings.Split(outputPath, "/")[:len(strings.Split(outputPath, "/"))-1], "/")
	f, err := os.Create(fmt.Sprintf("%s/%s.md", parentPath, makeAzureWikiFriendlyTitle(page.Title)))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	_, err = f.WriteString(converted)
	if err != nil {
		log.Fatal(err)
	}

	if hasChildren(page) {
		for _, child := range page.Children.Page.Results {
			handlePage(child.Links.Self, outputPath)
		}
	}
}

func downloadAttachmentsToLocalAndReplaceLinks(converted string, page confluence.Page) (result string, err error) {
	rx, err := regexp.Compile(fmt.Sprintf(`(%s)?/wiki/download/attachments/(\d+)/([0-9a-zA-Z.\-_%%]+)\?api=v2`, baseUrl))
	if err != nil {
		return result, err
	}
	imageLinks := rx.FindAllString(converted, -1)
	client := &http.Client{}
	for _, imageLink := range imageLinks {
		// download image
		normalizedImageLink := imageLink
		if !strings.HasPrefix(normalizedImageLink, "http") {
			normalizedImageLink = fmt.Sprintf("%s%s", baseUrl, normalizedImageLink)
		}
		imageUrl, err := url.ParseRequestURI(strings.TrimSpace(normalizedImageLink))
		if err != nil {
			return result, err
		}
		imageRequest, err := http.NewRequest("GET", fmt.Sprintf("%s", imageUrl.String()), nil)
		if err != nil {
			return result, err
		}
		imageRequest.Header.Set("Content-Type", "application/json")
		imageRequest.SetBasicAuth(username, token)
		imageResponse, err := client.Do(imageRequest)
		if err != nil {
			return result, err
		}

		imageBody, err := io.ReadAll(imageResponse.Body)
		if err != nil {
			return result, err
		}

		// save image
		imageName := fmt.Sprintf("%d-%s", rand.Int(), strings.Split(imageUrl.Path, "/")[len(strings.Split(imageUrl.Path, "/"))-1])
		imageFile, err := os.Create(fmt.Sprintf("%s/%s", attachmentsFolder, imageName))
		if err != nil {
			return result, err
		}
		defer imageFile.Close()
		if _, err := imageFile.Write(imageBody); err != nil {
			return result, err
		}
		if err := imageFile.Sync(); err != nil {
			return result, err
		}

		// replace image link
		converted = strings.ReplaceAll(converted, imageLink, fmt.Sprintf("%s/%s", attachmentsFolder, imageName))
	}
	return converted, nil
}

func fetchPage(u string) (p confluence.Page, err error) {
	client := &http.Client{}
	urlStruct, err := url.ParseRequestURI(u)
	if err != nil {
		return p, err
	}
	q := urlStruct.Query()
	q.Add("expand", "body.export_view,children.page")
	urlStruct.RawQuery = q.Encode()

	request, err := http.NewRequest("GET", fmt.Sprintf("%s", urlStruct.String()), nil)
	if err != nil {
		return p, err
	}

	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(username, token)
	response, err := client.Do(request)
	if err != nil {
		return p, err
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return p, err
	}

	var page confluence.Page

	err = json.Unmarshal(body, &page)
	if err != nil {
		return p, err
	}

	return page, nil
}

func init() {
	rootCmd.AddCommand(azureCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// azureCmd.PersistentFlags().String("foo", "", "A help for foo")

	azureCmd.Flags().StringVar(&cfgFile, "baseUrl", "", "base url for atlassian wiki")
	_ = azureCmd.MarkFlagRequired("baseUrl")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// azureCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
