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
	"fmt"
	"os"
	"strings"

	log "github.com/Sirupsen/logrus"

	"github.com/sendgrid/sendgrid-go"
	"github.com/sendgrid/sendgrid-go/helpers/mail"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

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

// Command execution
func send(cmd *cobra.Command, args []string) {
	debugCmd(cmd)
	fmt.Println("*** send called with:", len(args))

	from := createAddress(flagString(cmd, "from"))
	subject := flagString(cmd, "subject")
	if subject == "" {
		log.Fatal("The subject is required. You can get around this requirement if you use a template with a subject defined or if every personalization has a subject defined.")
	}
	toAddressesRaw := flagStringArray(cmd, "to")

	if len(toAddressesRaw) == 0 {
		log.Fatal("At lease one recepient should be present. Please -t or --to flag to specify a recepient.")
	}
	toAddresses := make([]*mail.Email, len(toAddressesRaw))
	for i, toRaw := range toAddressesRaw {
		toAddresses[i] = createAddress(toRaw)
	}

	to := toAddresses[0]
	var plainTextContent string
	if len(args) > 0 {
		plainTextContent = args[0]
	} else {
		plainTextContent = "and easy to do anywhere, even with Go"
	}
	htmlContent := "<strong>and easy to do anywhere, even with Go</strong>"
	apiKey := flagString(cmd, "key")
	if apiKey == "" {
		apiKey = os.Getenv("SENDGRID_API_KEY")
		if apiKey == "" {
			log.Fatal("SendGrid API Key is missing...")
		}
	}
	client := sendgrid.NewSendClient(apiKey)
	message := mail.NewSingleEmail(from, subject, to, plainTextContent, htmlContent)
	response, err := client.Send(message)
	if err != nil {
		log.Error(err)
	} else {
		if verbose || debug {
			log.Info("Status Code:", response.StatusCode)
			log.Info("Response Body:", response.Body)
			log.Info("Response Headers:")
			for k, v := range response.Headers {
				log.Infof("%s: %v", k, v)
			}
		}
	}
}

var (
	cfgFile string
	debug   bool
	verbose bool
)

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "sendgrid-cli",
	Short: "SendGrid CLI application",
	Long: `SendGrig CLI application that porvides email distribution with atttachmetns, templates,
and template parameter substitution.

to quickly create a Cobra application.`,
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
	RootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.sendgrid-cli.yaml)")

	RootCmd.PersistentFlags().BoolP("debug", "d", false, "Show full stack trace on error.")
	RootCmd.PersistentFlags().BoolP("verbose", "V", false, "Show more verbose details.")
	RootCmd.PersistentFlags().BoolP("plain", "p", false, "Print result as plain text (where applicable).")
	RootCmd.PersistentFlags().BoolP("json", "j", false, "Print result as JSON (where applicable).")
	RootCmd.PersistentFlags().StringP("key", "k", "", "SendGrid API Key.")
	RootCmd.PersistentFlags().StringP("from", "f", "", "from address")
	RootCmd.PersistentFlags().StringArrayP("to", "t", []string{}, "to address")
	RootCmd.PersistentFlags().StringArray("cc", []string{}, "to address")
	RootCmd.PersistentFlags().StringP("subject", "s", "", "email subject")
	RootCmd.PersistentFlags().StringP("body", "b", "", "HTML body file name")
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
			fmt.Println(err)
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
		fmt.Println(title)
		fmt.Println(strings.Repeat("=", len(title)))
		cmd.DebugFlags()
	}
}
