# Dedicated preview state; bootstrap bucket security before init. No credentials here.
terraform {
  backend "s3" {
    bucket       = "ai-outfitter-voice-state-216577824627-us-east-1"
    key          = "nonprod/public-media/terraform.tfstate"
    region       = "us-east-1"
    profile      = "unsup-nonprod-admin"
    encrypt      = true
    use_lockfile = true
  }
}
