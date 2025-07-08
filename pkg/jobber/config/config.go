package config

import (
	"time"

	"github.com/kyma-project/cloud-manager/pkg/config"
)

type JobberConfigStruct struct {
	Image string `json:"image" yaml:"image"`

	Namespace          string `json:"namespace" yaml:"namespace"`
	ServiceAccountName string `json:"serviceAccountName" yaml:"serviceAccountName"`

	PodRunningTimeout             time.Duration `json:"podRunningTimeout" yaml:"podRunningTimeout"`
	TerminationGracePeriodSeconds int64         `json:"terminationGracePeriodSeconds" yaml:"terminationGracePeriodSeconds"`

	ReadinessProbe ProbeConfig `json:"readinessProbe" yaml:"readinessProbe"`
	LivenessProbe  ProbeConfig `json:"livenessProbe" yaml:"livenessProbe"`

	ConfigMapConfig        string `json:"configMapConfig" yaml:"configMapConfig"`
	ConfigMapEnv           string `json:"configMapEnv" yaml:"configMapEnv"`
	SecretGcpCredentials   string `json:"secretGcpCredentials" yaml:"secretGcpCredentials"`
	SecretAwsCredentials   string `json:"secretAwsCredentials" yaml:"secretAwsCredentials"`
	SecretAzureCredentials string `json:"secretAzureCredentials" yaml:"secretAzureCredentials"`
	SecretCceeCredentials  string `json:"secretCceeCredentials" yaml:"secretCceeCredentials"`
}

type ProbeConfig struct {
	InitialDelaySeconds int32  `json:"initialDelaySeconds" yaml:"initialDelaySeconds"`
	PeriodSeconds       int32  `json:"periodSeconds" yaml:"periodSeconds"`
	Path                string `json:"path" yaml:"path"`
	Port                int32  `json:"port" yaml:"port"`
}

var JobberConfig = &JobberConfigStruct{}

func InitConfig(cfg config.Config) {
	cfg.Path(
		"jobber.config",
		config.Bind(JobberConfig),
		config.SourceFile("jobber.yaml"),

		config.Path(
			"image",
			config.SourceEnv("IMAGE"),
		),
		config.Path(
			"namespace",
			config.SourceEnv("NAMESPACE"),
			config.DefaultScalar("kcp-system"),
		),
		config.Path(
			"serviceAccountName",
			config.SourceEnv("SERVICE_ACCOUNT_NAME"),
			config.DefaultScalar("cloud-manager"),
		),

		config.Path(
			"podRunningTimeout",
			config.DefaultScalar("90m"),
		),
		config.Path(
			"terminationGracePeriodSeconds",
			config.DefaultScalar(630),
		),

		config.Path(
			"readinessProbe",
			config.DefaultObj(&ProbeConfig{
				InitialDelaySeconds: 5,
				PeriodSeconds:       10,
				Path:                "/readyz",
				Port:                8081,
			}),
		),
		config.Path(
			"livenessProbe",
			config.DefaultObj(&ProbeConfig{
				InitialDelaySeconds: 15,
				PeriodSeconds:       20,
				Path:                "/healthz",
				Port:                8081,
			}),
		),

		config.Path(
			"configMapConfig",
			config.DefaultScalar("cloud-manager-config"),
		),
		config.Path(
			"configMapEnv",
			config.DefaultScalar("cloud-manager-env"),
		),
		config.Path(
			"secretGcpCredentials",
			config.DefaultScalar("cloud-manager-gcp-vso"),
		),
		config.Path(
			"secretAwsCredentials",
			config.DefaultScalar("cloud-manager-aws-vso"),
		),
		config.Path(
			"secretAzureCredentials",
			config.DefaultScalar("cloud-manager-azure-vso"),
		),
		config.Path(
			"secretCceeCredentials",
			config.DefaultScalar("cloud-manager-ccee-vso"),
		),
	)
}
