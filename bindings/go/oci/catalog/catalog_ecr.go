package catalog

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"

	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// ecrHostPattern matches a private Amazon ECR host of the form
// <account-id>.dkr.ecr.<region>.amazonaws.com. Capture group one is the AWS
// region. ECR Public (public.ecr.aws) is a separate registry and is not
// matched here.
var ecrHostPattern = regexp.MustCompile(`(?i)^[0-9]+\.dkr\.ecr\.([a-z0-9-]+)\.amazonaws\.com$`)

// ecrDescribeAPI is the subset of the ECR client that the cataloger needs. It
// matches ecr.DescribeRepositoriesAPIClient, so a fake can satisfy it in unit
// tests and the cataloger never calls AWS.
type ecrDescribeAPI interface {
	DescribeRepositories(ctx context.Context, params *ecr.DescribeRepositoriesInput, optFns ...func(*ecr.Options)) (*ecr.DescribeRepositoriesOutput, error)
}

// newECRClient builds an ECR API client for the given region from the loaded
// AWS config. It is a package-private var so a unit test can inject a fake
// client and never reach AWS.
var newECRClient = func(ctx context.Context, opts CatalogerOptions, region string) (ecrDescribeAPI, error) {
	cfg, err := loadECRAWSConfig(ctx, opts, region)
	if err != nil {
		return nil, err
	}
	return ecr.NewFromConfig(cfg), nil
}

func init() {
	RegisterCataloger(ecrFactory{})
}

// ecrFactory builds an Amazon ECR cataloger for private ECR hosts.
type ecrFactory struct{}

var _ CatalogerFactory = ecrFactory{}

// Match reports whether host is a private Amazon ECR host of the form
// <account-id>.dkr.ecr.<region>.amazonaws.com.
func (ecrFactory) Match(host string) bool {
	return ecrHostPattern.MatchString(host)
}

// New builds an ECR cataloger. ECR does not serve the standard Distribution
// catalog endpoint, so the cataloger uses the ECR control-plane API
// DescribeRepositories through aws-sdk-go-v2. New parses the region from the
// host. The AWS config and the credentials are resolved lazily when
// Repositories runs.
func (ecrFactory) New(opts CatalogerOptions) (RepositoryCataloger, error) {
	region := ecrRegion(opts.Host)
	if region == "" {
		return nil, fmt.Errorf("host %q is not a valid amazon ecr host", opts.Host)
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger = logger.With(slog.String("realm", "ecr-cataloger"), slog.String("host", opts.Host), slog.String("region", region))

	return &ecrCataloger{
		opts:   opts,
		region: region,
		logger: logger,
	}, nil
}

// ecrCataloger lists repository names of a private Amazon ECR registry through
// the ECR control-plane API DescribeRepositories.
type ecrCataloger struct {
	opts   CatalogerOptions
	region string
	logger *slog.Logger
}

var _ RepositoryCataloger = (*ecrCataloger)(nil)

// Repositories lists the raw repository names of the registry, for example
// component-descriptors/github.com/acme/hello. The generic ComponentLister
// applies the component-descriptors/ filter and the subPath strip, so this
// method returns the raw repository names. ECR paginates through its own
// NextToken, so the cataloger drives the DescribeRepositories paginator and
// ignores the last cursor.
func (c *ecrCataloger) Repositories(ctx context.Context, _ string, fn func(repos []string) error) error {
	client, err := newECRClient(ctx, c.opts, c.region)
	if err != nil {
		return err
	}

	paginator := ecr.NewDescribeRepositoriesPaginator(client, &ecr.DescribeRepositoriesInput{})
	for paginator.HasMorePages() {
		c.logger.DebugContext(ctx, "describing ecr repositories")

		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("failed to describe ecr repositories: %w", err)
		}

		repos := make([]string, 0, len(page.Repositories))
		for _, repository := range page.Repositories {
			if repository.RepositoryName != nil {
				repos = append(repos, *repository.RepositoryName)
			}
		}

		if err := fn(repos); err != nil {
			return err
		}
	}

	return nil
}

// loadECRAWSConfig loads the AWS config for the given region. It prefers the
// static credentials from opts.Credentials when they are present. Otherwise it
// falls back to the default AWS credential chain, which reads environment
// variables, shared config files, and instance roles.
func loadECRAWSConfig(ctx context.Context, opts CatalogerOptions, region string) (aws.Config, error) {
	loadOptions := []func(*config.LoadOptions) error{config.WithRegion(region)}
	if provider, ok := ecrStaticCredentials(opts.Credentials); ok {
		loadOptions = append(loadOptions, config.WithCredentialsProvider(provider))
	}

	cfg, err := config.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load aws config for ecr: %w", err)
	}
	return cfg, nil
}

// ecrStaticCredentials maps OCM credentials to an AWS static credentials
// provider. AWS SigV4 needs an access key id and a secret access key, so
// static credentials require the Username (access key id) and the Password
// (secret access key). The optional AccessToken becomes the session token. It
// reports false when the username or the password is missing, so the caller
// falls back to the default AWS credential chain, which reads environment
// variables, shared config files, and instance roles.
func ecrStaticCredentials(creds *credsv1.OCICredentials) (aws.CredentialsProvider, bool) {
	if creds == nil {
		return nil, false
	}
	if creds.Username == "" || creds.Password == "" {
		return nil, false
	}
	return credentials.NewStaticCredentialsProvider(creds.Username, creds.Password, creds.AccessToken), true
}

// ecrRegion returns the AWS region parsed from an Amazon ECR host, or an empty
// string when the host is not a valid ECR host.
func ecrRegion(host string) string {
	match := ecrHostPattern.FindStringSubmatch(host)
	if match == nil {
		return ""
	}
	return match[1]
}
