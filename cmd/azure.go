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
	"path/filepath"
	"regexp"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	mdconverter "github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"github.com/jd-bv/confluence-to-markdown/confluence"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var baseUrl string
var baseUrlWiki string
var attachmentsFolder string
var pageIdToLink map[string]string

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

var debug = false

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

		var err error
		baseUrl, err = cmd.Flags().GetString("baseUrl")
		baseUrlWiki = fmt.Sprintf("%s", baseUrl)
		log.Println("base url: ", baseUrl)
		cobra.CheckErr(err)
		_, err = url.ParseRequestURI(baseUrlWiki)
		cobra.CheckErr(err)

		pageIdToLink = make(map[string]string)

		// Create root output folder
		if err := os.Mkdir(space, 0755); err != nil {
			log.Fatal(err)
		}

		attachmentsFolder = fmt.Sprintf("%s/__attachments", space)
		// attachments
		if err := os.Mkdir(attachmentsFolder, 0755); err != nil {
			log.Fatal(err)
		}

		client := &http.Client{}

		request, err := http.NewRequest("GET", fmt.Sprintf("%s/rest/api/space/%s", baseUrlWiki, space), nil)
		cobra.CheckErr(err)
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		if additionalHeaders != nil {
			for k, v := range additionalHeaders {
				request.Header.Set(k, v)
			}
		}

		response, err := client.Do(request)
		cobra.CheckErr(err)
		if response.StatusCode != 200 {
			log.Fatalf("HTTP error while fetching page: %d, \"%s\"", response.StatusCode, response.Status)
		}

		body, err := io.ReadAll(response.Body)
		cobra.CheckErr(err)

		var data confluence.Space
		if err = json.Unmarshal(body, &data); err != nil {
			cobra.CheckErr(err)
		}

		handlePage(fmt.Sprintf("%s%s", baseUrlWiki, data.Expandable.Homepage), space)
		replaceLinks(space)
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

func replaceLinks(dir string) {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(path, fmt.Sprintf("%s/%s", dir, "__attachments")) {
			return nil
		}
		fmt.Println(path, info.Size())
		replaceLink(path)
		return nil
	})
	if err != nil {
		fmt.Println(err)
	}
}

func replaceLink(filepath string) error {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("error reading file %s: %w", filepath, err)
	}
	contentString := string(content)
	rx, err := regexp.Compile(fmt.Sprintf(`(%s)\/pages\/viewpage\.action\?pageId=(\d+)`, baseUrl))
	if err != nil {
		return fmt.Errorf("error creating regex: %w", err)
	}

	links := rx.FindAllString(string(content), -1)
	fmt.Println("find all strings: ", links)

	for _, link := range links {
		fmt.Println("link: ", link)

		parts := strings.Split(link, "?pageId=")
		if len(parts) == 2 {
			fmt.Println("parts: ", parts)
			if newPagePath, exists := pageIdToLink[parts[1]]; exists {
				fmt.Println("new page path is: ", newPagePath)

				contentString = strings.ReplaceAll(contentString, link, newPagePath)
			}
		}

	}

	err = os.WriteFile(filepath, []byte(contentString), 0)
	if err != nil {
		return fmt.Errorf("error writing file with link replacement: %w", err)
	}

	return nil
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
	converter := mdconverter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			table.NewTablePlugin(),
		),
	)
	converted, err := converter.ConvertString(page.Body.ExportView.Value)
	if err != nil {
		log.Fatal(err)
	}

	// find all attachments, and download them to local folder
	converted, err = downloadAttachmentsToLocalAndReplaceLinks(converted, page)
	if err != nil {
		log.Fatal(err)
	}

	// write to file
	parentPath := strings.Join(strings.Split(outputPath, "/")[:len(strings.Split(outputPath, "/"))-1], "/")
	filename := makeAzureWikiFriendlyTitle(page.Title)
	filePath := fmt.Sprintf("%s/%s.md", parentPath, filename)
	f, err := os.Create(filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	pageIdToLink[page.Id] = filename

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
	rx, err := regexp.Compile(fmt.Sprintf(`(%s)?\/download\/attachments(.*)api=v2`, baseUrl))
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
		imageRequest.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

		if additionalHeaders != nil {
			for k, v := range additionalHeaders {
				imageRequest.Header.Set(k, v)
			}
		}
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
		imageName = strings.ReplaceAll(imageName, " ", "_")
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
		converted = strings.ReplaceAll(converted, imageLink, fmt.Sprintf("%s/%s", "__attachments", imageName))
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

	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	if additionalHeaders != nil {
		for k, v := range additionalHeaders {
			request.Header.Set(k, v)
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return p, err
	}
	if response.StatusCode != 200 {
		return p, errors.New(fmt.Sprintf("Fetch page failed: status %d, \"%s\"", response.StatusCode, response.Status))
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
