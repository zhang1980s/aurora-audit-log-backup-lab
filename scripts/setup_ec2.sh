#!/bin/bash
# EC2 instance setup script

# Update system packages
sudo dnf update -y

# Install MySQL client
sudo dnf install -y mariadb105

# Install AWS CLI
sudo dnf install -y aws-cli

# Install sysbench from source
sudo dnf groupinstall -y "Development Tools"
sudo dnf install -y mariadb105-devel openssl-devel git
git clone https://github.com/akopytov/sysbench.git
cd sysbench
./autogen.sh
./configure
make -j
sudo make install

# Configure AWS CLI
echo "Please configure AWS CLI with appropriate credentials:"
aws configure

echo "EC2 instance setup complete!"