// Copyright © 2017 Radomirs Cirskis <nad2000@gmail.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"

	"os"
	"regexp"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/jaytaylor/html2text"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	debug   bool
	verbose bool
)

// read into a string whole content of a file
func readFile(filename string) string {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Errorf("Failed to read the file %q", filename)
		log.Fatal(err)
	}
	return string(b)
}

// Creates email address structure form the given value in differnt formats:
// "Full Name <name@domain.name>" OR "name@domain.name"
func createAddress(raw string) *mail.Email {
	if raw == "" {
		log.Fatal("Missing email adderess.")
	}
	parts := strings.Split(raw, " <")
	if len(parts) == 0 || parts[0] == "" {
		log.Fatal("Email address is incorrect:", raw)
	}
	for i, p := range parts {
		parts[i] = strings.Trim(p, " ><")
	}
	if len(parts) < 2 {
		return mail.NewEmail(parts[0], parts[0])
	}
	return mail.NewEmail(parts[0], parts[1])
}

// Search in the arguments for HTML body and plain-text body.
func messageBodies(args []string) (htmlBody, plainBody string) {
	if len(args) == 0 {
		log.Fatal(`Missing message body.

Need to have at least one specified either with --html and/or --plain options
or postional parameters.`)
	}
	for i, b := range args {
		// body contains HTML tags:
		matched, err := regexp.MatchString("\\<[\\w]{1,}[^>]*\\>", b)
		if matched {
			htmlBody = b
			if len(args) == 1 {
				plainBody, err = html2text.FromString(b, html2text.Options{PrettyTables: true})
				if err != nil {
					log.Error("Failed to convert HTML body into plain-text:", err)
				}
				return
			}
			if i == 0 && args[1] != "" {
				plainBody = args[1]
			} else if i == 1 && args[0] != "" {
				plainBody = args[0]
			} else {
			}
			return
		}
	}
	if args[0] == "" {
		log.Fatal("Missing message body.")
	}
	return "", args[0]
}

// Command execution
func send(cmd *cobra.Command, args []string) {
	debugCmd(cmd)

	if len(args) > 2 {
		log.Fatalf("Too many positional argumets: %v", args)
	}

	from := createAddress(flagString(cmd, "from"))
	subject := flagString(cmd, "subject")
	if subject == "" {
		log.Fatal(`The subject is required. You can get around this requirement if you use 
a template with a subject defined or if every personalization has a subject defined.`)
	}
	toAddressesRaw := flagStringArray(cmd, "to")

	if len(toAddressesRaw) == 0 {
		log.Fatal("At lease one recepient should be present. Please -t or --to flag to specify a recepient.")
	}
	toAddresses := make([]*mail.Email, len(toAddressesRaw))
	for i, toRaw := range toAddressesRaw {
		toAddresses[i] = createAddress(toRaw)
	}

	ccAddressesRaw := flagStringArray(cmd, "cc")
	ccAddresses := make([]*mail.Email, len(ccAddressesRaw))
	for i, toRaw := range toAddressesRaw {
		toAddresses[i] = createAddress(toRaw)
	}

	var htmlContent, plainTextContent, templateID string
	htmlFilename, plainTextFilename := flagString(cmd, "html"), flagString(cmd, "plain")
	templateID = flagString(cmd, "template-id")
	if htmlFilename != "" || plainTextFilename != "" {
		if htmlFilename != "" {
			htmlContent = readFile(htmlFilename)
		}
		if plainTextFilename != "" {
			plainTextContent = readFile(plainTextFilename)
		} else {
			plainTextContent, _ = html2text.FromString(htmlContent, html2text.Options{PrettyTables: true})
		}
	} else if templateID == "" || len(args) > 0 {
		htmlContent, plainTextContent = messageBodies(args)
	} else {
		if templateID == "" {
			log.Fatal("Need to have at least one way of providing the message content.")
		}
		htmlContent = "<!-- Dummy Content -->" // A work arround to user template
	}

	apiKey := flagString(cmd, "key")
	username := flagString(cmd, "user")
	password := flagString(cmd, "password")

	if apiKey == "" && username == "" {
		apiKey = os.Getenv("SENDGRID_API_KEY")
		if apiKey == "" {
			log.Info("Missing username. Please use --user and --password options.")
			log.Info("Missing Sendgrid API key. Use --key option.")
			log.Fatal("Ether Sandgrid API Key or username and password should be present.")
		}
	}
	client := sendgrid.NewSendClient(apiKey)
	message := mail.NewSingleEmail(from, subject, toAddresses[0], plainTextContent, htmlContent)
	if len(toAddresses) > 1 {
		message.Personalizations[0].AddTos(toAddresses[1:]...)
	}
	if len(ccAddresses) > 0 {
		message.Personalizations[0].AddCCs(ccAddresses...)
	}

	for _, attFilename := range flagStringArray(cmd, "att") {
		b, err := ioutil.ReadFile(attFilename)
		if err != nil {
			log.Errorf("Failed to read the attachment %q", attFilename)
			log.Fatal(err)
		}
		a := mail.NewAttachment()
		a.SetType(mime.TypeByExtension(attFilename))
		a.SetDisposition("attachment")
		a.SetFilename(attFilename)
		a.SetContent(base64.StdEncoding.EncodeToString(b))
		message.AddAttachment(a)
		if debug {
			log.Debugf("Adding the atttachmetn %q", attFilename)
		}
	}

	if templateID != "" {
		message.SetTemplateID(templateID)
		for _, sub := range flagStringArray(cmd, "sub") {
			parts := strings.SplitN(sub, "=", 2)
			if len(parts) != 2 {
				log.Fatalf("Incorrect substitution: %s", flagStringArray(cmd, "sub"))
			}
			if debug {
				log.Debugf("Added substitution %q with the value %q", parts[0], parts[1])
			}
			message.Personalizations[0].SetSubstitution("[%"+parts[0]+"%]", parts[1])
		}
	}

	if apiKey == "" {
		sendV2(username, password, message)
		return
	}
	response, err := client.Send(message)
	if err != nil {
		log.Error("Failed to send the message.")
		log.Error(err)
	} else {
		if verbose || debug {
			log.Info("Status Code:", response.StatusCode)
			log.Info("Response Body:", response.Body)
			log.Info("Response Headers:")
			log.Info("=================")
			for k, v := range response.Headers {
				log.Infof("%s: %v", k, v)
			}
		}
	}
}

func newMultipPartForm(urlStr string, values url.Values, filenames []string) (*http.Request, error) {

	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)

	for _, fn := range filenames {
		file, err := os.Open(fn)
		defer file.Close()

		if err != nil {
			return nil, err
		}
		fileContents, err := ioutil.ReadAll(file)
		if err != nil {
			return nil, err
		}
		fi, err := file.Stat()
		if err != nil {
			return nil, err
		}
		part, err := writer.CreateFormFile(fn, fi.Name())
		if err != nil {
			return nil, err
		}
		part.Write(fileContents)
	}

	for key, vals := range values {
		if len(vals) == 1 {
			_ = writer.WriteField(key, vals[0])
		} else {
			for _, v := range vals {
				_ = writer.WriteField(key+"[]", v)
			}
		}
	}

	err := writer.Close()
	if err != nil {
		return nil, err
	}

	return http.NewRequest("POST", urlStr, body)
}

func addressToLists(emails []*mail.Email) ([]string, []string) {
	addresses := make([]string, len(emails))
	addressNames := make([]string, len(emails))
	for i, e := range emails {
		addresses[i] = e.Address
		addressNames[i] = e.Name
	}
	return addresses, addressNames
}

func sendV2(username, password string, m *mail.SGMailV3) {

	values := url.Values{
		"api_user": {username},
		"api_key":  {password},
		"subject":  {m.Subject},
		"from":     {m.From.Address},
		"fromname": {m.From.Name},
	}
	values["to"], values["toname"] = addressToLists(m.Personalizations[0].To)
	cc := m.Personalizations[0].CC
	if len(cc) > 0 {
		values["cc"], values["ccname"] = addressToLists(cc)
	}
	for _, c := range m.Content {
		if c.Type == "text/html" && c.Value != "" {
			values.Add("html", c.Value)
		}
		if c.Type == "text/plain" && c.Value != "" {
			values.Add("text", c.Value)
		}
	}

	filenames := make([]string, len(m.Attachments))
	for i, a := range m.Attachments {
		filenames[i] = a.Filename
	}
	// for _, a := range m.Attachments {
	// 	values["files["+a.Name+"]; filename="+a.Filename+" ;type="+a.Type, a.Content)
	// }

	request, err := newMultipPartForm("https://api.sendgrid.com/api/mail.send.json", values, filenames)
	if err != nil {
		log.Error("Failed to create the API request.")
		log.Fatal(err)
	}

	// if debug {
	// 	defer request.Body.Close()
	// 	body, _ := ioutil.ReadAll(request.Body)
	// 	log.Debug(string(body))
	// }

	client := &http.Client{}
	resp, err := client.Do(request)
	if err != nil {
		log.Error("Failed to send the email.")
		log.Fatal(err)
	} else {
		var bodyContent []byte
		log.Infof("Status Code: %d", resp.StatusCode)
		if debug {
			log.Info("Headers:")
			log.Info("========")
			for k, v := range resp.Header {
				log.Infof("%s:\t%v", k, v)
			}
			resp.Body.Read(bodyContent)
			resp.Body.Close()
			log.Info(bodyContent)
		}
	}
}

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "sendgrid-cli [flags] [HTML Content] [Plain text content]",
	Short: "SendGrid CLI application",
	Long: `SendGrig CLI application that porvides email distribution with atttachmetns, templates,
and template parameter substitution.

The content of the email can be specified either using position parameters or option --html / --plain, eg, 

sendgrid-cli -k API-KEY -t recepient@domain.net -f sender@foo.bar -s "The subject" "Dear recepien, <br/><p>..."

in this case the HTML content will get converted into the plain text version and added to the message.


sendgrid-cli -k API-KEY -t recepient@domain.net -f sender@foo.bar -s "The subject" -b FILENAME.html
sendgrid-cli -k API-KEY -t recepient@domain.net -f sender@foo.bar -s "The subject" -T TEMPLATE-ID -S "name=John Doe" -S "price=$42"

Instead of -k API-KEY you can user --user/-U with --password/-P.
`,
	Run: send,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.
	RootCmd.PersistentFlags().StringVar(&cfgFile, "config", "",
		"config file (default is $HOME/.sendgrid-cli.yaml)")

	RootCmd.PersistentFlags().BoolP("debug", "d", false, "Show full stack trace on error.")
	RootCmd.PersistentFlags().BoolP("verbose", "V", false, "Show more verbose details.")
	RootCmd.PersistentFlags().BoolP("json", "j", false, "Print result as JSON (where applicable).")
	RootCmd.PersistentFlags().StringP("key", "k", "",
		"SendGrid API Key (can set using environment variable SENDGRID_API_KEY).")
	RootCmd.PersistentFlags().StringP("user", "U", "", "Sendgrid user name.")
	RootCmd.PersistentFlags().StringP("password", "P", "", "Sendgrid user password.")
	RootCmd.PersistentFlags().StringP("from", "f", "sendgrid-cli@nowitworks.eu", "FROM address.")
	RootCmd.PersistentFlags().StringArrayP("to", "t", []string{}, "TO address (can be multiple).")
	RootCmd.PersistentFlags().StringArray("cc", []string{}, "CC address (can be multiple).")
	RootCmd.PersistentFlags().StringArrayP("att", "a", []string{}, "Attachment (can be multiple).")
	RootCmd.PersistentFlags().StringP("subject", "s", "", "Email subject.")
	RootCmd.PersistentFlags().StringP("html", "b", "", "HTML body file name.")
	RootCmd.PersistentFlags().StringP("plain", "p", "", "Plain-text body file name.")
	RootCmd.PersistentFlags().StringP("template-id", "T", "", "Sendgrid template ID.")
	RootCmd.PersistentFlags().StringArrayP("sub", "S", nil,
		"Template paramter substitution, eg, --sub ':name=Jhon Doe'")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			log.Error(err)
			os.Exit(1)
		}

		// Search config in home directory with name ".sendgrid-cli" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".sendgrid-cli")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}

func flagString(cmd *cobra.Command, name string) string {
	return cmd.Flag(name).Value.String()
}

func flagStringSlice(cmd *cobra.Command, name string) (val []string) {
	val, err := cmd.Flags().GetStringSlice(name)
	if err != nil {
		log.Fatal(err)
	}
	return
}

func flagStringArray(cmd *cobra.Command, name string) (val []string) {
	val, err := cmd.Flags().GetStringArray(name)
	if err != nil {
		log.Fatal(err)
	}
	return
}

func flagBool(cmd *cobra.Command, name string) (val bool) {
	val, err := cmd.Flags().GetBool(name)
	if err != nil {
		log.Fatal(err)
	}
	return
}

func flagInt(cmd *cobra.Command, name string) (val int) {
	val, err := cmd.Flags().GetInt(name)
	if err != nil {
		log.Fatal(err)
	}
	return
}

func debugCmd(cmd *cobra.Command) {
	debug = flagBool(cmd, "debug")
	verbose = flagBool(cmd, "verbose")

	if debug {
		log.SetLevel(log.DebugLevel)
		title := fmt.Sprintf("Command %q called with flags:", cmd.Name())
		log.Info(title)
		log.Info(strings.Repeat("=", len(title)))
		cmd.DebugFlags()
	}
}
