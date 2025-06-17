package jobber

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/clock"
)

type Option func(o *Options)

type Options struct {
	Clock clock.Clock
	Image string

	PodRunningTimeout             time.Duration
	TerminationGracePeriodSeconds int64

	ReadinessInitialDelaySeconds int32
	ReadinessPeriodSeconds       int32
	ReadinessPath                string
	ReadinessPort                int32

	LivenessInitialDelaySeconds int32
	LivenessPeriodSeconds       int32
	LivenessPath                string
	LivenessPort                int32

	ConfigMapConfig        string // cloud-manager-config
	ConfigMapEnv           string // cloud-manager-env
	SecretGcpCredentials   string
	SecretAwsCredentials   string
	SecretAzureCredentials string
	SecretCceeCredentials  string
}

func NewOptions(opts ...Option) *Options {
	o := &Options{}
	withDefaults(o)
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func WithClock(clock clock.Clock) Option {
	return func(o *Options) {
		o.Clock = clock
	}
}

func WithPodRunningTimeout(to time.Duration) Option {
	return func(o *Options) {
		o.PodRunningTimeout = to
	}
}

func withDefaults(o *Options) {
	o.Clock = &clock.RealClock{}
	o.Image = JobberConfig.Image
	o.PodRunningTimeout = JobberConfig.PodRunningTimeout
	o.TerminationGracePeriodSeconds = JobberConfig.TerminationGracePeriodSeconds
	o.ConfigMapConfig = JobberConfig.ConfigMapConfig
	o.ConfigMapEnv = JobberConfig.ConfigMapEnv
	o.SecretGcpCredentials = JobberConfig.SecretGcpCredentials
	o.SecretAwsCredentials = JobberConfig.SecretAwsCredentials
	o.SecretAzureCredentials = JobberConfig.SecretAzureCredentials
	o.SecretCceeCredentials = JobberConfig.SecretCceeCredentials
}

func (o *Options) IsPodRunningTimeout(pod *corev1.Pod) bool {
	duration := o.Clock.Since(pod.CreationTimestamp.Time)
	if duration > o.PodRunningTimeout {
		return true
	}
	return false
}

func (o *Options) Volumes() []corev1.Volume {
	return []corev1.Volume{
		{
			Name: "gcp-credentials",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: o.SecretGcpCredentials,
				},
			},
		},
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							ConfigMap: &corev1.ConfigMapProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: o.ConfigMapConfig,
								},
							},
						},
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: o.SecretAwsCredentials,
								},
							},
						},
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: o.SecretAzureCredentials,
								},
							},
						},
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: o.SecretCceeCredentials,
								},
							},
						},
					},
				},
			},
		},
	}
}

func (o *Options) VolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      "gcp-credentials",
			MountPath: "/var/run/secrets/cloud-manager.kyma-project.io/gcp",
			ReadOnly:  true,
		},
		{
			Name:      "config",
			MountPath: "/var/cloud-manager.kyma-project.io/config",
		},
	}
}

func (o *Options) EnvVars() []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "GCP_SA_JSON_KEY_PATH",
			Value: "/var/run/secrets/cloud-manager.kyma-project.io/gcp/credentials.json",
		},
		{
			Name:  "GCP_VPC_PEERING_KEY_PATH",
			Value: "/var/run/secrets/cloud-manager.kyma-project.io/gcp/credentials-vpcpeering.json",
		},
		{
			Name:  "CONFIG_DIR",
			Value: "/var/cloud-manager.kyma-project.io/config",
		},
		{
			Name:  "FEATURE_FLAG_CONFIG_FILE",
			Value: "/var/cloud-manager.kyma-project.io/config/featureFlags.yaml",
		},
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
		{
			Name: "NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
	}
}

func (o *Options) EnvFrom() []corev1.EnvFromSource {
	return []corev1.EnvFromSource{
		{
			ConfigMapRef: &corev1.ConfigMapEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: o.ConfigMapEnv,
				},
			},
		},
	}
}

func (o *Options) ReadinessProbe() *corev1.Probe {
	return &corev1.Probe{
		InitialDelaySeconds: JobberConfig.ReadinessProbe.InitialDelaySeconds, // 5
		PeriodSeconds:       JobberConfig.ReadinessProbe.PeriodSeconds,       // 10
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: JobberConfig.ReadinessProbe.Path,                   // /readyz
				Port: intstr.FromInt32(JobberConfig.ReadinessProbe.Port), // 8081
			},
		},
	}
}

func (o *Options) LivenessProbe() *corev1.Probe {
	return &corev1.Probe{
		InitialDelaySeconds: JobberConfig.LivenessProbe.InitialDelaySeconds, // 15
		PeriodSeconds:       JobberConfig.LivenessProbe.PeriodSeconds,       // 20
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: JobberConfig.LivenessProbe.Path,                   // /healthz
				Port: intstr.FromInt32(JobberConfig.LivenessProbe.Port), // 8081
			},
		},
	}
}
