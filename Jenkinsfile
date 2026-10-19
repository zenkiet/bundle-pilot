pipeline {
  agent { label 'built-in' }
  options {
    timestamps()
    disableConcurrentBuilds()
    timeout(time: 60, unit: 'MINUTES')
    buildDiscarder(logRotator(numToKeepStr: '20', artifactNumToKeepStr: '10'))
  }
  stages {
    stage('Setup') { steps { sh '.jenkins/scripts/setup.sh' } }
    stage('Verify') { steps { sh '.jenkins/scripts/verify.sh' } }
    stage('Build') {
      when { not { buildingTag() } }
      steps { sh '.jenkins/scripts/build.sh' }
    }
    stage('Release') {
      when { buildingTag() }
      environment {
        GITHUB_TOKEN = credentials('github-token')
        GHCR_TOKEN = credentials('ghcr-token')
        NPM_TOKEN = credentials('npm-token')
      }
      steps { sh '.jenkins/scripts/release.sh' }
    }
    stage('npm') {
      when { buildingTag() }
      environment { NPM_TOKEN = credentials('npm-token') }
      steps { sh '.jenkins/scripts/npm.sh' }
    }
  }
  post {
    always {
      archiveArtifacts artifacts: 'release/*.tar.gz,release/*.zip,release/checksums.txt', allowEmptyArchive: true, fingerprint: true
    }
    cleanup { deleteDir() }
  }
}
