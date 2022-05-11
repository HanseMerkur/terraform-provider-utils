// Package autodoc contains the engine for the autodoc command line application
// to automatically generate mkdocs-style documentation for the provider. This
// application uses text templates and feeds them the parsed schema data to
// produce up-to-date documentation.
//
// This application takes the following arguments:
//   -provider=NAME
//     Name of the Terraform provider. Defaults to "Terraform Provider".
//   -root
//     The root directory to being placing output documentation files. Defaults
//     to the current working directory. The mkdocs.yml file will be placed
//     in this location.
//   -docs-dir
//     The name of the directory to place generated documentation. This will
//     be placed under the parameter supplied for -root. Defaults to 'docs'.
//     The autogenerated mkdocs.yml file will have its 'docs_dir' set to this
//     value.
//   -templates-dir
//     The directory to search for template files. Templates are searched
//     and loaded recursively from this directory. Defaults to
//     '$(cwd)/templates'
//   -template-ext
//     File extension for template files. Defaults to '.template'
//
// Arguments can be assigned values by using the '=' operator:
//   $> autodoc -root='/my/path'
//
// This application will exit 1 on error, 0 on success.
//
// The following files are generated as output by the application. Let
// $(cwd) be the value supplied to -root, and $(docs) be the value supplied
// to -docs-dir:
//   1. $(cwd)/mkdocs.yml
//     mkdocs configuration file
//   2. $(cwd)/$(docs)/index.md
//     provider documentation file
//   3. $(cwd)/$(docs)/resources/*.md
//     All resource documentation. There will be one md file for each resource.
//     The resource files will be named corresponding to its name in the
//     provider's ResourcesMap.
//   4. $(cwd)/$(docs)/data-sources/*.md
//     All datasource documentation. There will be one md file for each
//     datasource.  The datasource files will be named corresponding to its
//     name in the provider's DataSourcesMap.
//
// This application assumes the user has read/write access to all output paths
//
// This application uses the following template associations for each output
// file:
//   mkdocs.yml.template
//     $(cwd)/mkdocs.yml => mkdocs configuration
//   index.md.template
//     $(cwd)/$(docs)/index.md => Provider documentation
//   resource.md.template
//     $(cwd)/$(docs)/resources/*.md => Documentation for all resources
//   datasource.md.template
//     $(cwd)/$(docs)/data-sources/*.md => Documentation for all data sources
package autodoc

import (
	"fmt"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Exit status constants
const (
	// Exit status denoting success
	ExitSuccess = 0
	// Exit status denoting error
	ExitError = 1
)

// Document is the entry point into autodoc execution. The command
// line arguments and templates are read and parsed. The provider reference
// is parsed to generate the documentation. This function will return a list
// of errors.  If this list is empty, no errors were encountered.
func Document(provider *schema.Provider) []error {
	errors := []error{}

	// Parse command line arguments into concrete struct representation
	args, argsErr := parseArgs()
	if argsErr != nil {
		errors = append(errors, argsErr)
		return errors
	}

	if args.help {
		Usage()
		return errors
	}

	// Using the parsed arguments, recursively load all the templates from
	// the specified directory
	templates, tmplErr := parseTemplates(args)
	if tmplErr != nil {
		errors = append(errors, tmplErr)
		return errors
	}

	// Creates a bidirectional error channel. This is for communication
	// across the goroutines. As goroutines are spun up to generate the
	// documentation, they communicate their error status back through this
	// channel
	errChan := make(chan error, 1)

	// Total number of go routines. This signals how many outputs to receive
	// on the error channel before exiting.
	totalGoroutines := 0

	// generate mkdocs.yml file
	totalGoroutines += 1
	go generateMkdocsYml(
		mkdocsYmlDoc{
			goroutineBase: goroutineBase{
				outFile: filepath.Join(
					"mkdocs.yml",
				),
				template:     templates,
				templateName: mkdocsYmlTemplate + args.templateFileExt,
				errChan:      errChan,
			},
			provider: provider,
			args:     args,
		},
	)

	// generate index.md for provider documentation
	totalGoroutines += 1
	go generateSchemaDoc(
		schemaDoc{
			goroutineBase: goroutineBase{
				outFile: filepath.Join(
					args.docsDir,
					"index.md",
				),
				template:     templates,
				templateName: providerMdTemplate + args.templateFileExt,
				errChan:      errChan,
			},
			schemaType: typeProvider,
			name:       args.providerName,
			schema:     provider.Schema,
		},
	)

	// generate resource documentation for each resource
	for name, resource := range provider.ResourcesMap {
		totalGoroutines += 1
		go generateSchemaDoc(
			schemaDoc{
				goroutineBase: goroutineBase{
					outFile: filepath.Join(
						args.docsDir,
						"resources",
						name+".md",
					),
					template:     templates,
					templateName: resourceMdTemplate + args.templateFileExt,
					errChan:      errChan,
				},
				schemaType: typeResource,
				name:       name,
				schema:     resource.Schema,
			},
		)
	}

	// generate data source documentation for each data source
	for name, resource := range provider.DataSourcesMap {
		totalGoroutines += 1
		go generateSchemaDoc(
			schemaDoc{
				goroutineBase: goroutineBase{
					outFile: filepath.Join(
						args.docsDir,
						"data-sources",
						name+".md",
					),
					template:     templates,
					templateName: dataSourceMdTemplate + args.templateFileExt,
					errChan:      errChan,
				},
				schemaType: typeDataSource,
				name:       name,
				schema:     resource.Schema,
			},
		)
	}

	// Wait for output from the go routines and start building the error list
	for i := 0; i < totalGoroutines; i++ {
		err := <-errChan
		if err != nil {
			errors = append(errors, err)
		}
	}
	return errors
}

// Usage prints usage information to stdout
func Usage() {
	fmt.Println(
		`AUTODOC

NAME
  autodoc - Generate mkdocs style documentation for a Terraform provider

SYNOPSIS
  autodoc [options] [arguments]

DESCRIPTION
  autodoc generates the necessary config files for mkdocs and parses
  the provider definition to generate markdown files. The following files
  are created:

    * mkdocs.yml       => mkdocs configuration
    * docs/index.md    => Provider documentation
    * resources/*.md   => documentation for each resource
    * data-sources/*.md => documentation for each data source

  autodoc uses templates to generate the markdown files. autodoc makes
  the following template associations:

    * mkdocs.yml       => mkdocs.yml.template
    * docs/index.md    => index.md.template
    * resources/*.md   => resource.md.template
    * data-sources/*.md => datasource.md.template

  Templates are written in golang stdlib template. See pkg/text/template
  for more information.

  autodoc exits 0 on succes, 1 on error.

OPTIONS
  -help
    Display usage and exit.

ARGUMENTS
  -provider=NAME
	  Name of the Terraform provider. Defaults to "Terraform Provider".
  -root=ROOT_DIR
    Path to direct generated documentation files. mkdocs.yml will be
    written to this location. Defaults to current working directory.
  -docs-dir=DOCS_DIR
    Name of the documentation directory. The mkdocs.yml's docs_dir will
    set to this value. All markdown files will be under this directory.
    This value is relative to -root. Defaults to 'docs'
  -templates-dir=TEMPLATES_DIR
    Name of the templates directory. The autodoc tool will load all
    template files recursively from this location. This value is relative
    to -root. Defaults to 'templates'
  -templates-ext=TEMPLATES_EXT
    Extension for template files. Defaults to '.template'.
`,
	)
}
