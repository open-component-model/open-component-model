package catalog

import (
	"context"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	credsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// fakeECRAPI is a fake ECR client. It serves two repository pages linked
// through NextToken and records the NextToken of every request so a test can
// assert pagination.
type fakeECRAPI struct {
	page1, page2 []string
	calls        []string
}

func (f *fakeECRAPI) DescribeRepositories(_ context.Context, params *ecr.DescribeRepositoriesInput, _ ...func(*ecr.Options)) (*ecr.DescribeRepositoriesOutput, error) {
	token := aws.ToString(params.NextToken)
	f.calls = append(f.calls, token)

	switch token {
	case "":
		return &ecr.DescribeRepositoriesOutput{
			Repositories: ecrRepositories(f.page1),
			NextToken:    aws.String("page2"),
		}, nil
	case "page2":
		return &ecr.DescribeRepositoriesOutput{
			Repositories: ecrRepositories(f.page2),
		}, nil
	default:
		return nil, fmt.Errorf("unexpected next token %q", token)
	}
}

// ecrRepositories builds a slice of ECR repository objects from repository
// names.
func ecrRepositories(names []string) []ecrtypes.Repository {
	repos := make([]ecrtypes.Repository, 0, len(names))
	for _, name := range names {
		repos = append(repos, ecrtypes.Repository{RepositoryName: aws.String(name)})
	}
	return repos
}

// injectFakeECRClient points newECRClient at fake for the duration of the test
// and restores the original afterwards, so no test reaches AWS.
func injectFakeECRClient(t *testing.T, fake ecrDescribeAPI) {
	t.Helper()
	original := newECRClient
	newECRClient = func(_ context.Context, _ CatalogerOptions, _ string) (ecrDescribeAPI, error) {
		return fake, nil
	}
	t.Cleanup(func() { newECRClient = original })
}

func TestECRFactory_Match(t *testing.T) {
	f := ecrFactory{}
	for _, host := range []string{
		"123456789012.dkr.ecr.us-east-1.amazonaws.com",
		"000000000000.dkr.ecr.eu-central-1.amazonaws.com",
		"123456789012.DKR.ECR.US-EAST-1.AMAZONAWS.COM",
	} {
		assert.Truef(t, f.Match(host), "expected match for %q", host)
	}
	for _, host := range []string{
		"public.ecr.aws",
		"docker.io",
		"ghcr.io",
		"example.com",
		"dkr.ecr.us-east-1.amazonaws.com",
		"123456789012.dkr.ecr.us-east-1.amazonaws.com.evil.com",
		"123456789012.dkr.ecr.us-east-1.example.com",
	} {
		assert.Falsef(t, f.Match(host), "expected no match for %q", host)
	}
}

func TestECRRegion(t *testing.T) {
	assert.Equal(t, "us-east-1", ecrRegion("123456789012.dkr.ecr.us-east-1.amazonaws.com"))
	assert.Equal(t, "eu-central-1", ecrRegion("000000000000.dkr.ecr.eu-central-1.amazonaws.com"))
	assert.Equal(t, "", ecrRegion("ghcr.io"))
}

func TestECRCataloger_PaginatesAndAssembles(t *testing.T) {
	fake := &fakeECRAPI{
		page1: []string{"component-descriptors/componentB", "not-a-component"},
		page2: []string{"component-descriptors/componentC"},
	}
	injectFakeECRClient(t, fake)

	cataloger, err := ecrFactory{}.New(CatalogerOptions{
		Host: "123456789012.dkr.ecr.us-east-1.amazonaws.com",
	})
	require.NoError(t, err)

	var repos []string
	err = cataloger.Repositories(t.Context(), "", func(page []string) error {
		repos = append(repos, page...)
		return nil
	})
	require.NoError(t, err)

	// The raw repository names concatenate across both pages. Pagination
	// follows the ECR NextToken.
	assert.Equal(t, []string{
		"component-descriptors/componentB",
		"not-a-component",
		"component-descriptors/componentC",
	}, repos)

	// The first request carries no next token and the second request carries
	// the next token of the first page.
	assert.Equal(t, []string{"", "page2"}, fake.calls)
}

func TestECRCataloger_ListComponentsThroughLister(t *testing.T) {
	fake := &fakeECRAPI{
		page1: []string{
			"myteam/component-descriptors/componentB",
			"myteam/component-descriptors/componentA",
			"myteam/not-a-component",
			"other/component-descriptors/componentX",
		},
		page2: []string{"myteam/component-descriptors/componentC"},
	}
	injectFakeECRClient(t, fake)

	cataloger, err := ecrFactory{}.New(CatalogerOptions{
		Host:    "123456789012.dkr.ecr.us-east-1.amazonaws.com",
		SubPath: "myteam",
	})
	require.NoError(t, err)

	lister := NewComponentLister(cataloger, "myteam")

	var names []string
	err = lister.ListComponents(t.Context(), "", func(found []string) error {
		names = append(names, found...)
		return nil
	})
	require.NoError(t, err)

	// Each page is deduplicated and sorted, then the pages concatenate. The
	// component-descriptors/ filter drops not-a-component, the subPath prefix
	// scopes the match to myteam and is stripped, and the other/ repository is
	// dropped because it does not match the subPath.
	assert.Equal(t, []string{"componentA", "componentB", "componentC"}, names)
}

func TestECRFactory_New_RejectsNonECRHost(t *testing.T) {
	_, err := ecrFactory{}.New(CatalogerOptions{Host: "ghcr.io"})
	require.Error(t, err)
}

func TestECRStaticCredentials(t *testing.T) {
	_, ok := ecrStaticCredentials(nil)
	assert.False(t, ok)

	_, ok = ecrStaticCredentials(&credsv1.OCICredentials{})
	assert.False(t, ok)

	// An access token alone is not valid AWS static credentials, so the
	// cataloger falls back to the default AWS credential chain.
	_, ok = ecrStaticCredentials(&credsv1.OCICredentials{AccessToken: "session"})
	assert.False(t, ok)

	// A username without a password is not enough for AWS SigV4.
	_, ok = ecrStaticCredentials(&credsv1.OCICredentials{Username: "AKID"})
	assert.False(t, ok)

	provider, ok := ecrStaticCredentials(&credsv1.OCICredentials{Username: "AKID", Password: "secret"})
	require.True(t, ok)
	creds, err := provider.Retrieve(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "AKID", creds.AccessKeyID)
	assert.Equal(t, "secret", creds.SecretAccessKey)

	// The optional access token becomes the session token.
	provider, ok = ecrStaticCredentials(&credsv1.OCICredentials{Username: "AKID", Password: "secret", AccessToken: "session"})
	require.True(t, ok)
	creds, err = provider.Retrieve(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "session", creds.SessionToken)
}
