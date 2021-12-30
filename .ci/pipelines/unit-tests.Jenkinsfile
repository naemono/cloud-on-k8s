pipeline {

    agent {
        label 'linux'
    }

    options {
        timeout(time: 1, unit: 'HOURS')
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
