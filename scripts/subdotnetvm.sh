#!/bin/bash

MAIN_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )
source "$MAIN_PATH"/scripts/constants.sh
#clean logs
rm ~/hackathon/subdotnet_draft/logs/*.log
# build subdotnet
rm /tmp/subdotnet -r
dotnet publish ~/hackathon/subdotnet_draft/src/subdotnet.csproj 
#copy binary
cp /tmp/subdotnet ${build_dir} -r
#fixes problem with libgrpc_csharp_ext.x64.so shared library
export LD_LIBRARY_PATH="${build_dir}/subdotnet"

# Create genesis
subdotnet_genesis_path="${build_dir}/subdotnet/genesis.txt"
touch $subdotnet_genesis_path
echo "2hDRXLhsaKJYkfcM1F6Cp2djxDDwAwmWanjnTEzbiE5W1UVDqX" > $subdotnet_genesis_path

source "$MAIN_PATH"/scripts/run.sh $subdotnetvm_path $subdotnet_genesis_path

