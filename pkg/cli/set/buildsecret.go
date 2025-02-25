package set

import (
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	kcmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/templates"

	buildv1 "github.com/openshift/api/build/v1"
)

var (
	buildSecretLong = templates.LongDesc(`
		Set or remove a build secret on a build config.

		A build config can reference a secret to push or pull images from private registries or
		to access private source repositories.

		Specify the type of secret being set by using the --push, --pull, or --source flags.
		A secret reference can be removed by using --remove flag.

		A label selector may be specified with the --selector flag to select the build configs
		on which to set or remove secrets. Alternatively, all build configs in the namespace can
		be selected with the --all flag.`)

	buildSecretExample = templates.Examples(`
		# Clear the push secret on a build config
		oc set build-secret --push --remove bc/mybuild

		# Set the pull secret on a build config
		oc set build-secret --pull bc/mybuild mysecret

		# Set the push and pull secret on a build config
		oc set build-secret --push --pull bc/mybuild mysecret

		# Set the source secret on a set of build configs matching a selector
		oc set build-secret --source -l app=myapp gitsecret`)
)

type BuildSecretOptions struct {
	PrintFlags *genericclioptions.PrintFlags

	Selector string
	All      bool
	Local    bool
	Push     bool
	Pull     bool
	Source   bool
	Remove   bool

	Printer           printers.ResourcePrinter
	Builder           func() *resource.Builder
	Namespace         string
	ExplicitNamespace bool
	Resources         []string
	SecretArg         string
	DryRunStrategy    kcmdutil.DryRunStrategy
	FieldManager      string

	resource.FilenameOptions
	genericclioptions.IOStreams
}

func NewBuildSecretOptions(streams genericclioptions.IOStreams) *BuildSecretOptions {
	return &BuildSecretOptions{
		PrintFlags: genericclioptions.NewPrintFlags("secret updated").WithTypeSetter(scheme.Scheme),
		IOStreams:  streams,
	}
}

// NewCmdBuildSecret implements the set build-secret command
func NewCmdBuildSecret(f kcmdutil.Factory, streams genericclioptions.IOStreams) *cobra.Command {
	o := NewBuildSecretOptions(streams)
	cmd := &cobra.Command{
		Use:     "build-secret BUILDCONFIG SECRETNAME",
		Short:   "Update a build secret on a build config",
		Long:    buildSecretLong,
		Example: buildSecretExample,
		Run: func(cmd *cobra.Command, args []string) {
			kcmdutil.CheckErr(o.Complete(f, cmd, args))
			kcmdutil.CheckErr(o.Validate())
			kcmdutil.CheckErr(o.Run())
		},
	}
	usage := "to use to edit the resource"
	kcmdutil.AddFilenameOptionFlags(cmd, &o.FilenameOptions, usage)
	cmd.Flags().StringVarP(&o.Selector, "selector", "l", o.Selector, "Selector (label query) to filter build configs")
	cmd.Flags().BoolVar(&o.All, "all", o.All, "If true, select all build configs in the namespace")
	cmd.Flags().BoolVar(&o.Push, "push", o.Push, "If true, set the push secret on a build config")
	cmd.Flags().BoolVar(&o.Pull, "pull", o.Pull, "If true, set the pull secret on a build config")
	cmd.Flags().BoolVar(&o.Source, "source", o.Source, "If true, set the source secret on a build config")
	cmd.Flags().BoolVar(&o.Remove, "remove", o.Remove, "If true, remove the build secret.")
	cmd.Flags().BoolVar(&o.Local, "local", o.Local, "If true, set build-secret will NOT contact api-server but run locally.")

	o.PrintFlags.AddFlags(cmd)
	kcmdutil.AddDryRunFlag(cmd)
	kcmdutil.AddFieldManagerFlagVar(cmd, &o.FieldManager, "kubectl-set")

	return cmd
}

var supportedBuildTypes = []string{"buildconfigs"}

func (o *BuildSecretOptions) secretFromArg(arg string) (string, error) {
	builder := o.Builder().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		LocalParam(o.Local).
		NamespaceParam(o.Namespace).DefaultNamespace().
		RequireObject(false).
		ContinueOnError().
		ResourceNames("secrets", arg).
		Flatten()

	var secretName string
	err := builder.Do().Visit(func(info *resource.Info, err error) error {
		if err != nil {
			return err
		}
		if info.Mapping.Resource.GroupResource() != (schema.GroupResource{Resource: "secrets"}) {
			return fmt.Errorf("please specify a secret")
		}
		secretName = info.Name
		return nil
	})
	if err != nil {
		return "", err
	}
	return secretName, nil
}

func (o *BuildSecretOptions) Complete(f kcmdutil.Factory, cmd *cobra.Command, args []string) error {
	if !o.Remove {
		if len(args) < 1 {
			return kcmdutil.UsageErrorf(cmd, "a secret name must be specified")
		}
		o.SecretArg = args[len(args)-1]
		args = args[:len(args)-1]
	}
	o.Resources = args
	if len(o.Resources) == 0 && len(o.Selector) == 0 && len(o.Filenames) == 0 && !o.All {
		return kcmdutil.UsageErrorf(cmd, "one or more build configs must be specified as <name> or <resource>/<name>")
	}

	var err error
	o.Namespace, o.ExplicitNamespace, err = f.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	o.DryRunStrategy, err = kcmdutil.GetDryRunStrategy(cmd)
	if err != nil {
		return err
	}

	o.Builder = f.NewBuilder

	kcmdutil.PrintFlagsWithDryRunStrategy(o.PrintFlags, o.DryRunStrategy)
	o.Printer, err = o.PrintFlags.ToPrinter()
	if err != nil {
		return err
	}

	return nil
}

func (o *BuildSecretOptions) Validate() error {
	if !o.Pull && !o.Push && !o.Source {
		return fmt.Errorf("specify the type of secret to set (--push, --pull, or --source)")
	}
	if !o.Remove && len(o.SecretArg) == 0 {
		return fmt.Errorf("specify a secret to set")
	}
	if o.Remove && len(o.SecretArg) > 0 {
		return fmt.Errorf("a secret cannot be specified when using the --remove flag")
	}
	if o.Local && o.DryRunStrategy == kcmdutil.DryRunServer {
		return fmt.Errorf("cannot specify --local and --dry-run=server - did you mean --dry-run=client?")
	}
	return nil
}

func (o *BuildSecretOptions) Run() error {
	var (
		secret string
		err    error
	)
	if len(o.SecretArg) > 0 {
		secret, err = o.secretFromArg(o.SecretArg)
		if err != nil {
			return err
		}
	}

	b := o.Builder().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		LocalParam(o.Local).
		ContinueOnError().
		NamespaceParam(o.Namespace).DefaultNamespace().
		FilenameParam(o.ExplicitNamespace, &o.FilenameOptions).
		Flatten()

	if !o.Local {
		b = b.
			ResourceNames("buildconfigs", o.Resources...).
			LabelSelectorParam(o.Selector).
			Latest()
		if o.All {
			b = b.ResourceTypes(supportedBuildTypes...).SelectAllParam(o.All)
		}
	}

	singleItemImplied := false
	infos, err := b.Do().IntoSingleItemImplied(&singleItemImplied).Infos()
	if err != nil {
		return err
	}

	patches := CalculatePatchesExternal(infos, func(info *resource.Info) (bool, error) {
		bc, ok := info.Object.(*buildv1.BuildConfig)
		if !ok {
			return false, nil
		}
		o.updateBuildConfig(bc, secret)
		return true, nil
	})

	if singleItemImplied && len(patches) == 0 {
		return fmt.Errorf("cannot set a build secret on %s/%s", infos[0].Mapping.Resource, infos[0].Name)
	}

	allErrs := []error{}
	for _, patch := range patches {
		info := patch.Info
		name := getObjectName(info)
		if patch.Err != nil {
			allErrs = append(allErrs, fmt.Errorf("error: %s %v\n", name, patch.Err))
			continue
		}

		if string(patch.Patch) == "{}" || len(patch.Patch) == 0 {
			klog.V(1).Infof("info: %s was not changed\n", name)
			continue
		}

		if o.Local || o.DryRunStrategy == kcmdutil.DryRunClient {
			if err := o.Printer.PrintObj(info.Object, o.Out); err != nil {
				allErrs = append(allErrs, err)
			}
			continue
		}

		actual, err := resource.NewHelper(info.Client, info.Mapping).
			DryRun(o.DryRunStrategy == kcmdutil.DryRunServer).
			WithFieldManager(o.FieldManager).
			Patch(info.Namespace, info.Name, types.StrategicMergePatchType, patch.Patch, &metav1.PatchOptions{})
		if err != nil {
			allErrs = append(allErrs, fmt.Errorf("failed to patch secret  %v", err))
			continue
		}

		if err := o.Printer.PrintObj(actual, o.Out); err != nil {
			allErrs = append(allErrs, err)
		}
	}
	return errors.NewAggregate(allErrs)
}

func (o *BuildSecretOptions) updateBuildConfig(bc *buildv1.BuildConfig, secret string) {
	if o.Push {
		if o.Remove {
			bc.Spec.Output.PushSecret = nil
		} else {
			bc.Spec.Output.PushSecret = &corev1.LocalObjectReference{
				Name: secret,
			}
		}
	}

	if o.Pull {
		switch {
		case bc.Spec.Strategy.DockerStrategy != nil:
			if o.Remove {
				bc.Spec.Strategy.DockerStrategy.PullSecret = nil
			} else {
				bc.Spec.Strategy.DockerStrategy.PullSecret = &corev1.LocalObjectReference{
					Name: secret,
				}
			}
		case bc.Spec.Strategy.SourceStrategy != nil:
			if o.Remove {
				bc.Spec.Strategy.SourceStrategy.PullSecret = nil
			} else {
				bc.Spec.Strategy.SourceStrategy.PullSecret = &corev1.LocalObjectReference{
					Name: secret,
				}
			}
		case bc.Spec.Strategy.CustomStrategy != nil:
			if o.Remove {
				bc.Spec.Strategy.CustomStrategy.PullSecret = nil
			} else {
				bc.Spec.Strategy.CustomStrategy.PullSecret = &corev1.LocalObjectReference{
					Name: secret,
				}
			}
		}
	}

	if o.Source {
		if o.Remove {
			bc.Spec.Source.SourceSecret = nil
		} else {
			bc.Spec.Source.SourceSecret = &corev1.LocalObjectReference{
				Name: secret,
			}
		}
	}
}
