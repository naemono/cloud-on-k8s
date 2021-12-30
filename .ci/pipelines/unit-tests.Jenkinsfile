pipeline {

    agent {
        label 'linux'
    }

    options {
        timeout(time: 1, unit: 'HOURS')
    }

    environment {
        GO111MODULE = 'on'
        VAULT_ADDR = credentials('vault-addr')
        VAULT_ROLE_ID = credentials('vault-role-id')
        VAULT_SECRET_ID = credentials('vault-secret-id')
        // read safely TAG_NAME, defined for a release build and not for a nightly build
        TAG_NAME = sh(script: 'echo -n $TAG_NAME', returnStdout: true)
    }

    stages {
        stage('Unit Tests') {
            steps {
                sh 'make -C .ci TARGET=unit CI_IMAGE=golang:1.16 ci'
            }
        }
    }

    post {
        cleanup {
            cleanWs()
        }
    }
}
