package ecs

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

type secretState struct {
	idExists     bool
	idARN        arn
	secretExists bool
	secretARN    arn
}

// Return the state of the akita.software secrets in the AWS secret manager.
func (wf *AddWorkflow) checkAkitaSecrets() (secretState, error) {
	state := secretState{}

	// TODO: use tags instead of name prefix?  We'll specify both when creating secret.
	input := &secretsmanager.ListSecretsInput{
		Filters: []types.Filter{
			{
				Key:    "name",
				Values: []string{akitaSecretPrefix},
			},
		},
	}
	svc := secretsmanager.NewFromConfig(wf.awsConfig)
	output, err := svc.ListSecrets(wf.ctx, input)
	if err != nil {
		return state, wrapUnauthorized(err)
	}

	for _, s := range output.SecretList {
		name := aws.ToString(s.Name)
		switch name {
		case defaultKeyIDName:
			state.idExists = true
			state.idARN = arn(aws.ToString(s.ARN))
		case defaultKeySecretName:
			state.secretExists = true
			state.secretARN = arn(aws.ToString(s.ARN))
		}
	}
	return state, nil
}

// Create an Akita text secret with tags to identify it later.
// Note that that permission to tag a secret is different than just creating one.
func (wf *AddWorkflow) createAkitaSecret(
	secretName string,
	secretText string,
	description string,
) (arn, error) {
	svc := secretsmanager.NewFromConfig(wf.awsConfig)
	input := &secretsmanager.CreateSecretInput{
		Name:         aws.String(secretName),
		Description:  aws.String(description),
		SecretString: aws.String(secretText),
		Tags: []types.Tag{
			{
				Key:   aws.String(akitaCreationTagKey),
				Value: aws.String(akitaCreationTagValue),
			},
		},
	}
	output, err := svc.CreateSecret(wf.ctx, input)
	if err != nil {
		return "", wrapUnauthorized(err)
	}

	return arn(aws.ToString(output.ARN)), nil
}
