package mpawselasticsearch

import (
	"flag"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	mp "github.com/mackerelio/go-mackerel-plugin"
)

const (
	nameSpace          = "AWS/ES"
	metricsTypeAverage = "Average"
	metricsTypeSum     = "Sum"
	metricsTypeMaximum = "Maximum"
	metricsTypeMinimum = "Minimum"
)

type metrics struct {
	Name string
	Type string
}

// ESPlugin mackerel plugin for aws elasticsearch
type ESPlugin struct {
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Domain          string
	ClientID        string
	CloudWatch      *cloudwatch.CloudWatch
	KeyPrefix       string
	LabelPrefix     string
}

// MetricKeyPrefix interface for PluginWithPrefix
func (p ESPlugin) MetricKeyPrefix() string {
	if p.KeyPrefix == "" {
		return "es"
	}
	return p.KeyPrefix
}

// MetricLabelPrefix ...
func (p ESPlugin) MetricLabelPrefix() string {
	if p.LabelPrefix == "" {
		return "AWS ES"
	}
	return p.LabelPrefix
}

func (p *ESPlugin) prepare() error {
	sess, err := session.NewSession()
	if err != nil {
		return err
	}

	config := aws.NewConfig()
	if p.AccessKeyID != "" && p.SecretAccessKey != "" {
		config = config.WithCredentials(credentials.NewStaticCredentials(p.AccessKeyID, p.SecretAccessKey, ""))
	}
	if p.Region != "" {
		config = config.WithRegion(p.Region)
	}

	p.CloudWatch = cloudwatch.New(sess, config)
	return nil
}

func (p ESPlugin) getLastPointFromCloudWatch(metric metrics) (*cloudwatch.Datapoint, error) {
	now := time.Now()

	dimensions := []*cloudwatch.Dimension{
		{
			Name:  aws.String("DomainName"),
			Value: aws.String(p.Domain),
		},
		{
			Name:  aws.String("ClientId"),
			Value: aws.String(p.ClientID),
		},
	}

	response, err := p.CloudWatch.GetMetricStatistics(&cloudwatch.GetMetricStatisticsInput{
		Dimensions: dimensions,
		StartTime:  aws.Time(now.Add(time.Duration(180) * time.Second * -1)),
		EndTime:    aws.Time(now),
		MetricName: aws.String(metric.Name),
		Period:     aws.Int64(60),
		Statistics: []*string{aws.String(metric.Type)},
		Namespace:  aws.String(nameSpace),
	})

	if err != nil {
		return nil, err
	}

	datapoints := response.Datapoints
	if len(datapoints) == 0 {
		return nil, nil
	}

	latest := new(time.Time)
	var latestDp *cloudwatch.Datapoint
	for _, dp := range datapoints {
		if dp.Timestamp.Before(*latest) {
			continue
		}

		latest = dp.Timestamp
		latestDp = dp
	}

	return latestDp, nil
}

func mergeStatFromDatapoint(stat map[string]float64, dp *cloudwatch.Datapoint, metric metrics) map[string]float64 {
	if dp != nil {
		var value float64
		if metric.Type == metricsTypeAverage {
			value = *dp.Average
		} else if metric.Type == metricsTypeSum {
			value = *dp.Sum
		} else if metric.Type == metricsTypeMaximum {
			value = *dp.Maximum
		} else if metric.Type == metricsTypeMinimum {
			value = *dp.Minimum
		}
		if metric.Name == "ClusterUsedSpace" || metric.Name == "MasterFreeStorageSpace" || metric.Name == "FreeStorageSpace" {
			// MBytes -> Bytes
			value = value * 1024 * 1024
		}
		stat[metric.Name] = value
	}
	return stat
}

// FetchMetrics interface for mackerelplugin
func (p ESPlugin) FetchMetrics() (map[string]float64, error) {
	stat := make(map[string]float64)

	for _, met := range [...]metrics{
		{Name: "ClusterStatus.green", Type: metricsTypeMinimum},
		{Name: "ClusterStatus.yellow", Type: metricsTypeMaximum},
		{Name: "ClusterStatus.red", Type: metricsTypeMaximum},
		{Name: "Nodes", Type: metricsTypeAverage},
		{Name: "SearchableDocuments", Type: metricsTypeAverage},
		{Name: "DeletedDocuments", Type: metricsTypeAverage},
		{Name: "CPUUtilization", Type: metricsTypeMaximum},
		{Name: "FreeStorageSpace", Type: metricsTypeMinimum},
		{Name: "ClusterUsedSpace", Type: metricsTypeMinimum},
		{Name: "ClusterIndexWritesBlocked", Type: metricsTypeMaximum},
		{Name: "JVMMemoryPressure", Type: metricsTypeMaximum},
		{Name: "AutomatedSnapshotFailure", Type: metricsTypeMaximum},
		{Name: "KibanaHealthyNodes", Type: metricsTypeMinimum},
		{Name: "MasterCPUUtilization", Type: metricsTypeMaximum},
		{Name: "MasterFreeStorageSpace", Type: metricsTypeSum},
		{Name: "MasterJVMMemoryPressure", Type: metricsTypeMaximum},
		{Name: "MasterReachableFromNode", Type: metricsTypeMinimum},
		{Name: "ReadLatency", Type: metricsTypeAverage},
		{Name: "WriteLatency", Type: metricsTypeAverage},
		{Name: "ReadThroughput", Type: metricsTypeAverage},
		{Name: "WriteThroughput", Type: metricsTypeAverage},
		{Name: "DiskQueueDepth", Type: metricsTypeAverage},
		{Name: "ReadIOPS", Type: metricsTypeAverage},
		{Name: "WriteIOPS", Type: metricsTypeAverage},
	} {
		v, err := p.getLastPointFromCloudWatch(met)
		if err == nil {
			stat = mergeStatFromDatapoint(stat, v, met)
		} else {
			log.Printf("%s: %s", met, err)
		}
	}

	return stat, nil
}

// GraphDefinition interface for mackerelplugin
func (p ESPlugin) GraphDefinition() map[string]mp.Graphs {
	labelPrefix := p.MetricLabelPrefix()
	return map[string]mp.Graphs{
		"ClusterStatus": {
			Label: (labelPrefix + " ClusterStatus"),
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "ClusterStatus.green", Label: "green"},
				{Name: "ClusterStatus.yellow", Label: "yellow"},
				{Name: "ClusterStatus.red", Label: "red"},
			},
		},
		"Nodes": {
			Label: (labelPrefix + " Nodes"),
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "Nodes", Label: "Nodes"},
			},
		},
		"SearchableDocuments": {
			Label: (labelPrefix + " SearchableDocuments"),
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "SearchableDocuments", Label: "SearchableDocuments"},
			},
		},
		"DeletedDocuments": {
			Label: (labelPrefix + " DeletedDocuments"),
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "DeletedDocuments", Label: "DeletedDocuments"},
			},
		},
		"CPUUtilization": {
			Label: (labelPrefix + " CPU Utilization"),
			Unit:  "percentage",
			Metrics: []mp.Metrics{
				{Name: "CPUUtilization", Label: "CPUUtilization"},
			},
		},
		"FreeStorageSpace": {
			Label: (labelPrefix + " Free Storage Space"),
			Unit:  "bytes",
			Metrics: []mp.Metrics{
				{Name: "FreeStorageSpace", Label: "FreeStorageSpace"},
			},
		},
		"ClusterUsedSpace": {
			Label: (labelPrefix + " Cluster Used Space"),
			Unit:  "bytes",
			Metrics: []mp.Metrics{
				{Name: "ClusterUsedSpace", Label: "ClusterUsedSpace"},
			},
		},
		"ClusterIndexWritesBlocked": {
			Label: (labelPrefix + " ClusterIndexWritesBlocked"),
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "ClusterIndexWritesBlocked", Label: "ClusterIndexWritesBlocked"},
			},
		},
		"JVMMemoryPressure": {
			Label: (labelPrefix + " JVMMemoryPressure"),
			Unit:  "percentage",
			Metrics: []mp.Metrics{
				{Name: "JVMMemoryPressure", Label: "JVMMemoryPressure"},
			},
		},
		"AutomatedSnapshotFailure": {
			Label: (labelPrefix + " AutomatedSnapshotFailure"),
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "AutomatedSnapshotFailure", Label: "AutomatedSnapshotFailure"},
			},
		},
		"KibanaHealthyNodes": {
			Label: (labelPrefix + " KibanaHealthyNodes"),
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "KibanaHealthyNodes", Label: "KibanaHealthyNodes"},
			},
		},
		"MasterCPUUtilization": {
			Label: (labelPrefix + " MasterCPUUtilization"),
			Unit:  "percentage",
			Metrics: []mp.Metrics{
				{Name: "MasterCPUUtilization", Label: "MasterCPUUtilization"},
			},
		},
		"MasterFreeStorageSpace": {
			Label: (labelPrefix + " MasterFreeStorageSpace"),
			Unit:  "bytes",
			Metrics: []mp.Metrics{
				{Name: "MasterFreeStorageSpace", Label: "MasterFreeStorageSpace"},
			},
		},
		"MasterJVMMemoryPressure": {
			Label: (labelPrefix + " MasterJVMMemoryPressure"),
			Unit:  "percentage",
			Metrics: []mp.Metrics{
				{Name: "MasterJVMMemoryPressure", Label: "MasterJVMMemoryPressure"},
			},
		},
		"MasterReachableFromNode": {
			Label: (labelPrefix + " MasterReachableFromNode"),
			Unit:  "percentage",
			Metrics: []mp.Metrics{
				{Name: "MasterReachableFromNode", Label: "MasterReachableFromNode"},
			},
		},
		"Latency": {
			Label: (labelPrefix + " Latency"),
			Unit:  "float",
			Metrics: []mp.Metrics{
				{Name: "ReadLatency", Label: "ReadLatency"},
				{Name: "WriteLatency", Label: "WriteLatency"},
			},
		},
		"Throughput": {
			Label: (labelPrefix + " Throughput"),
			Unit:  "bytes/sec",
			Metrics: []mp.Metrics{
				{Name: "ReadThroughput", Label: "ReadThroughput"},
				{Name: "WriteThroughput", Label: "WriteThroughput"},
			},
		},
		"DiskQueueDepth": {
			Label: (labelPrefix + " DiskQueueDepth"),
			Unit:  "integer",
			Metrics: []mp.Metrics{
				{Name: "DiskQueueDepth", Label: "DiskQueueDepth"},
			},
		},
		"IOPS": {
			Label: (labelPrefix + " IOPS"),
			Unit:  "iops",
			Metrics: []mp.Metrics{
				{Name: "ReadIOPS", Label: "ReadIOPS"},
				{Name: "WriteIOPS", Label: "WriteIOPS"},
			},
		},
	}
}

// Do the plugin
func Do() {
	optRegion := flag.String("region", "", "AWS Region")
	optAccessKeyID := flag.String("access-key-id", "", "AWS Access Key ID")
	optSecretAccessKey := flag.String("secret-access-key", "", "AWS Secret Access Key")
	optClientID := flag.String("client-id", "", "AWS Client ID")
	optDomain := flag.String("domain", "", "ES domain name")
	optTempfile := flag.String("tempfile", "", "Temp file name")
	optKeyPrefix := flag.String("metric-key-prefix", "es", "Metric key prefix")
	optLabelPrefix := flag.String("metric-label-prefix", "AWS ES", "Metric label prefix")
	flag.Parse()

	var es ESPlugin

	if *optRegion == "" {
		sess, err := session.NewSession()
		if err != nil {
			log.Fatalln(err)
		}
		ec2metadata := ec2metadata.New(sess)
		if ec2metadata.Available() {
			es.Region, _ = ec2metadata.Region()
		}
	} else {
		es.Region = *optRegion
	}

	es.Region = *optRegion
	es.Domain = *optDomain
	es.ClientID = *optClientID
	es.AccessKeyID = *optAccessKeyID
	es.SecretAccessKey = *optSecretAccessKey
	es.KeyPrefix = *optKeyPrefix
	es.LabelPrefix = *optLabelPrefix

	err := es.prepare()
	if err != nil {
		log.Fatalln(err)
	}

	helper := mp.NewMackerelPlugin(es)
	helper.Tempfile = *optTempfile

	helper.Run()
}
