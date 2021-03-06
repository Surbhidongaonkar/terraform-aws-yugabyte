package yugabyte

import (
	"crypto/tls"
	"fmt"
	"os"
	"strings"
	"path/filepath"
	"testing"
	"time"
	"io/ioutil"

	"github.com/Pallinder/go-randomdata"
	"github.com/gruntwork-io/terratest/modules/aws"
	http_helper "github.com/gruntwork-io/terratest/modules/http-helper"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/retry"
	"github.com/gruntwork-io/terratest/modules/ssh"
	"github.com/gruntwork-io/terratest/modules/terraform"
	test_structure "github.com/gruntwork-io/terratest/modules/test-structure"
	//"github.com/gruntwork-io/terratest/modules/random"
	"github.com/stretchr/testify/assert"

)
//vars for aws
var awsRegion, sshUser, yugabyteDir  string

func FindStringInResponse(statusCode int, body string) bool {
	if statusCode != 200 {
		return false
	}
	return strings.Contains(body, "YugabyteDB")
}

func TestYugaByteAwsTerraform(t *testing.T) {
	t.Parallel()

	yugabyteDir := test_structure.CopyTerraformFolderToTemp(t, "..", "../terraform-aws-yugabyte")
	maxRetries := 30
	timeBetweenRetries := 5 * time.Second

	defer test_structure.RunTestStage(t, "teardown", func() {
		terraformOptions := test_structure.LoadTerraformOptions(t, yugabyteDir)
		terraform.Destroy(t, terraformOptions)
		EC2Key := test_structure.LoadEc2KeyPair(t, yugabyteDir)
		aws.DeleteEC2KeyPair(t, EC2Key)
		os.RemoveAll(yugabyteDir)
	})

	test_structure.RunTestStage(t, "SetUp", func() {
		terraformOptions, EC2Key := configureTerraformOptions(t, yugabyteDir)
		test_structure.SaveTerraformOptions(t, yugabyteDir, terraformOptions)
		test_structure.SaveEc2KeyPair(t, yugabyteDir, EC2Key)
		terraform.InitAndApply(t, terraformOptions)
	})

	test_structure.RunTestStage(t, "validate", func() {
		terraformOptions := test_structure.LoadTerraformOptions(t, yugabyteDir)
		EC2Key := test_structure.LoadEc2KeyPair(t, yugabyteDir)
		hosts := terraform.Output(t, terraformOptions, "hostname")
		YugaByteHosts := strings.Fields(strings.Trim(hosts, "[]\"\""))
		sshUser:= terraform.Output(t, terraformOptions, "ssh_user")
		//Test master and tserver UI
		testYugaByteMasterURL(t, terraformOptions, maxRetries, timeBetweenRetries)
		testYugaByteTserverURL(t, terraformOptions, maxRetries, timeBetweenRetries)

		for _, host := range YugaByteHosts {
			fmt.Printf("type of host is %T", host)
			host = strings.Trim(host, "\"\",")
			logger.Logf(t, "Host is :- %s", host)

			//todo:check if condition 
			// if strings.Contains(host, "."){
			 	testYugaByteSSH(t, terraformOptions, maxRetries, timeBetweenRetries, sshUser, EC2Key, host)
				//testYugaByteDirectory(t, terraformOptions, maxRetries, timeBetweenRetries, sshUser, EC2Key, host)
			 	testYugaByteYSQLSH(t, terraformOptions, maxRetries, timeBetweenRetries, sshUser, EC2Key, host)
			 	testYugaByteCQLSH(t, terraformOptions, maxRetries, timeBetweenRetries, sshUser, EC2Key, host)
			 	testYugaByteConf(t, terraformOptions, maxRetries, timeBetweenRetries, sshUser, EC2Key, host, yugabyteDir)
			 	testYugaByteTserverProcess(t, terraformOptions, maxRetries, timeBetweenRetries, sshUser, EC2Key, host)
				testYugaByteMasterProcess(t, terraformOptions, maxRetries, timeBetweenRetries, sshUser, EC2Key, host)
			 	testYugaByteMasterLog(t, terraformOptions, maxRetries, timeBetweenRetries, sshUser, EC2Key, host)
			 	testYugaByteTserverLog(t, terraformOptions, maxRetries, timeBetweenRetries, sshUser, EC2Key, host)
			 //}
		}

	})
}


func configureTerraformOptions(t *testing.T, yugabyteDir string) (*terraform.Options, *aws.Ec2Keypair) {
	awsRegion = os.Getenv("AWS_REGION")
	clusterName := strings.ToLower(randomdata.FirstName(randomdata.Male))
	uniqueID := os.Getenv("GITHUB_RUN_ID")//random.UniqueId()
	fmt.Print(">>>>>>>>>>>>>>>>>>>>>>>>>"+uniqueID+">>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
	sshKeyPair := fmt.Sprintf("terratest-example-%s", uniqueID)
	EC2Key := aws.CreateAndImportEC2KeyPair(t, awsRegion, sshKeyPair)
	PrivateKey:= []byte(EC2Key.KeyPair.PrivateKey)
	RemoteDir:= filepath.Join(yugabyteDir, sshKeyPair)
	RemoteDir = RemoteDir+".pem"
	err := ioutil.WriteFile(RemoteDir , PrivateKey, 0400)
	if err != nil {
		t.Fatalf("Failed to save key to %s: %v", RemoteDir, err)
	}
	vpcID := os.Getenv("VPC_ID")
	availabilityZones := strings.Split(os.Getenv("AVAILABILITY_ZONES"), ",")
	subnetID := strings.Split(os.Getenv("SUBNET_IDS"), ",")

	terraformOptions := &terraform.Options{
		TerraformDir: yugabyteDir,

		Vars: map[string]interface{}{
			"region": awsRegion,
			"cluster_name": clusterName,	
			"ssh_keypair": sshKeyPair,
			"ssh_private_key": RemoteDir,
			"region_name": awsRegion,
			"vpc_id": vpcID, 
			"availability_zones": availabilityZones,
			"subnet_ids": subnetID,
					
		},
	}

	ybVersion, ok := os.LookupEnv("TAG_NAME")
	if ok {
		terraformOptions.Vars["yb_version"]= ybVersion
	}
	fmt.Printf("%+v", terraformOptions)
	return terraformOptions, EC2Key
}


func testYugaByteMasterURL(t *testing.T, terraformOptions *terraform.Options, maxRetries int, timeBetweenRetries time.Duration) {
	YugaByteURL := terraform.Output(t, terraformOptions, "master-ui")
	fmt.Print("..........................."+YugaByteURL)
	TLSConfig := tls.Config{}
	http_helper.HttpGetWithRetryWithCustomValidation(t, YugaByteURL, &TLSConfig, maxRetries, timeBetweenRetries, FindStringInResponse)
}

func testYugaByteTserverURL(t *testing.T, terraformOptions *terraform.Options, maxRetries int, timeBetweenRetries time.Duration) {
	YugaByteURL := terraform.Output(t, terraformOptions, "tserver-ui")

	TLSConfig := tls.Config{}
	http_helper.HttpGetWithRetryWithCustomValidation(t, YugaByteURL, &TLSConfig, maxRetries, timeBetweenRetries, FindStringInResponse)
}

func testYugaByteSSH(t *testing.T, terraformOptions *terraform.Options, maxRetries int, timeBetweenRetries time.Duration, sshUser string, EC2Key *aws.Ec2Keypair, host string) {

	sshHost := ssh.Host{
		Hostname:    host,
		SshKeyPair:  EC2Key.KeyPair,
		SshUserName: sshUser,
	}
	sampleText := "Hello World! Testing ssh commands"
	retry.DoWithRetry(t, "Attempting to SSH", maxRetries, timeBetweenRetries, func() (string, error) {
		output, err := ssh.CheckSshCommandE(t, sshHost, fmt.Sprintf("echo '%s'", sampleText))
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(sampleText) != strings.TrimSpace(output) {
			return "", fmt.Errorf("Expected: %s. Got: %s\n", sampleText, output)
		}
		return "", nil
	})

}

func testYugaByteDirectory(t *testing.T, terraformOptions *terraform.Options, maxRetries int, timeBetweenRetries time.Duration, sshUser string, EC2Key *aws.Ec2Keypair, host string) {

	RemoteDir := filepath.Join("/home/", sshUser)
	commandListDirectory := "ls -1 " + RemoteDir
	testFile := filepath.Join(yugabyteDir, "/test/DirectoryList")
	logger.Logf(t, "testFile :- %s", testFile)
	assert.FileExists(t, testFile)

	sshHost := ssh.Host{
		Hostname:   host,
		SshKeyPair:  EC2Key.KeyPair,
		SshUserName: sshUser,
	}

	file, err := ioutil.ReadFile(testFile)
	if err != nil {
		fmt.Print(err)
	}

	retry.DoWithRetry(t, "Listing Directory", maxRetries, timeBetweenRetries, func() (string, error) {
		output, err := ssh.CheckSshCommandE(t, sshHost, commandListDirectory)
		if err != nil {
			return "", err
		}
		logger.Logf(t, "Directory list :- %s", output)

		assert.Equal(t, strings.TrimSpace(output), strings.TrimSpace(string(file)))
		return output, nil
	})
}

func testYugaByteYSQLSH(t *testing.T, terraformOptions *terraform.Options, maxRetries int, timeBetweenRetries time.Duration, sshUser string, EC2Key *aws.Ec2Keypair, host string) {

	commandConnectYSQLSH := "cd " + filepath.Join("/home/", sshUser, "/yugabyte-db/tserver") + " && ./bin/ysqlsh  --echo-queries -h " + string(host)
	sshHost := ssh.Host{
		Hostname:    host,
		SshKeyPair:  EC2Key.KeyPair,
		SshUserName: sshUser,
	}

	retry.DoWithRetry(t, "Conecting YSQLSH", maxRetries, timeBetweenRetries, func() (string, error) {
		output, err := ssh.CheckSshCommandE(t, sshHost, commandConnectYSQLSH)
		if err != nil {
			return "", err
		}
		logger.Logf(t, "Output of Ysql command :- %s", output)
		return output, nil
	})

}

func testYugaByteCQLSH(t *testing.T, terraformOptions *terraform.Options, maxRetries int, timeBetweenRetries time.Duration, sshUser string, EC2Key *aws.Ec2Keypair, host string) {

	commandConnectCQLSH := "cd " + filepath.Join("/home/", sshUser, "/yugabyte-db/tserver") + " && ./bin/cqlsh " + string(host) + " 9042"
	sshHost := ssh.Host{
		Hostname:    string(host),
		SshKeyPair:  EC2Key.KeyPair,
		SshUserName: sshUser,
	}

	retry.DoWithRetry(t, "Conecting CQLSH", maxRetries, timeBetweenRetries, func() (string, error) {
		output, err := ssh.CheckSshCommandE(t, sshHost, commandConnectCQLSH)
		if err != nil {
			return "", err
		}
		logger.Logf(t, "Output of Cql command :- %s", output)
		return output, nil
	})

}

func testYugaByteConf(t *testing.T, terraformOptions *terraform.Options, maxRetries int, timeBetweenRetries time.Duration, sshUser string, EC2Key *aws.Ec2Keypair, host string, yugabyteDir string) {
	RemoteDir := filepath.Join("/home/", sshUser)
	sshHost := ssh.Host{
		Hostname:    string(host),
		SshKeyPair:  EC2Key.KeyPair,
		SshUserName: sshUser,
	}

	getTserverConf := ssh.ScpDownloadOptions{
		RemoteHost: sshHost,
		RemoteDir:  filepath.Join(RemoteDir, "/yugabyte-db/tserver/conf/"),
		LocalDir:   yugabyteDir,
	}

	getFileError := ssh.ScpDirFromE(t, getTserverConf, false)

	if getFileError != nil {
		logger.Logf(t, "We got error while getting file from server :- %s", getFileError)
	}

	assert.FileExists(t, filepath.Join(yugabyteDir, "/server.conf"))
}

func testYugaByteMasterProcess(t *testing.T, terraformOptions *terraform.Options, maxRetries int, timeBetweenRetries time.Duration, sshUser string, EC2Key *aws.Ec2Keypair, host string) {
	sshHost := ssh.Host{
		Hostname:    string(host),
		SshKeyPair:  EC2Key.KeyPair,
		SshUserName: sshUser,
	}
	sampleText := "yb-master"
	retry.DoWithRetry(t, "Checking master process", maxRetries, timeBetweenRetries, func() (string, error) {
		output, err := ssh.CheckSshCommandE(t, sshHost, fmt.Sprintf("pgrep --list-name yb-master"))
		if err != nil {
			return "", err
		}
		if ! strings.Contains(output, sampleText) {
			return "", fmt.Errorf("Expected: %s. Got: %s\n", sampleText, output)
		}
		return "", nil
	})

}

func testYugaByteTserverProcess(t *testing.T, terraformOptions *terraform.Options, maxRetries int, timeBetweenRetries time.Duration, sshUser string, EC2Key *aws.Ec2Keypair, host string) {
	sshHost := ssh.Host{
		Hostname:    string(host),
		SshKeyPair:  EC2Key.KeyPair,
		SshUserName: sshUser,
	}
	sampleText := "yb-tserver"
	retry.DoWithRetry(t, "Checkng tserver process", maxRetries, timeBetweenRetries, func() (string, error) {
		output, err := ssh.CheckSshCommandE(t, sshHost, fmt.Sprintf("pgrep --list-name yb-tserver"))
		if err != nil {
			return "", err
		}
		if ! strings.Contains(output, sampleText) {
			return "", fmt.Errorf("Expected: %s. Got: %s\n", sampleText, output)
		}
		return "", nil
	})

}

func testYugaByteMasterLog(t *testing.T, terraformOptions *terraform.Options, maxRetries int, timeBetweenRetries time.Duration, sshUser string, EC2Key *aws.Ec2Keypair, host string) {
	RemoteDir:= filepath.Join("/home/", sshUser, "/yugabyte-db/master")
	sshHost := ssh.Host{
		Hostname:    string(host),
		SshKeyPair:  EC2Key.KeyPair,
		SshUserName: sshUser,
	}
	//Checking master.out
	sampleText := "master.out"
	retry.DoWithRetry(t, "Checking master log file master.out", maxRetries, timeBetweenRetries, func() (string, error) {
		output, err := ssh.CheckSshCommandE(t, sshHost, fmt.Sprintf("ls %s | grep master.out",RemoteDir))
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(sampleText) != strings.TrimSpace(output) {
			return "", fmt.Errorf("Expected: %s. Got: %s\n", sampleText, output)
		}
		return "", nil
	})

	//checking master.err
	sampleText = "master.err"
	retry.DoWithRetry(t, "Checking master log file master.err", maxRetries, timeBetweenRetries, func() (string, error) {
		output, err := ssh.CheckSshCommandE(t, sshHost, fmt.Sprintf("ls %s | grep master.err",RemoteDir))
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(sampleText) != strings.TrimSpace(output) {
			return "", fmt.Errorf("Expected: %s. Got: %s\n", sampleText, output)
		}
		return "", nil
	})

}

func testYugaByteTserverLog(t *testing.T, terraformOptions *terraform.Options, maxRetries int, timeBetweenRetries time.Duration, sshUser string, EC2Key *aws.Ec2Keypair, host string) {
	RemoteDir:= filepath.Join("/home/", sshUser, "/yugabyte-db/tserver")
	sshHost := ssh.Host{
		Hostname:    string(host),
		SshKeyPair:  EC2Key.KeyPair,
		SshUserName: sshUser,
	}
	sampleText := "tserver.out"
	retry.DoWithRetry(t, "Checking tserver log file tserver.out", maxRetries, timeBetweenRetries, func() (string, error) {
		output, err := ssh.CheckSshCommandE(t, sshHost, fmt.Sprintf("ls %s | grep tserver.out",RemoteDir))
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(sampleText) != strings.TrimSpace(output) {
			return "", fmt.Errorf("Expected: %s. Got: %s\n", sampleText, output)
		}
		return "", nil
	})

	sampleText = "tserver.err"
	retry.DoWithRetry(t, "Checking tserver log file tserver.err", maxRetries, timeBetweenRetries, func() (string, error) {
		output, err := ssh.CheckSshCommandE(t, sshHost, fmt.Sprintf("ls %s | grep tserver.err", RemoteDir))
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(sampleText) != strings.TrimSpace(output) {
			return "", fmt.Errorf("Expected: %s. Got: %s\n", sampleText, output)
		}
		return "", nil
	})

}



