
# seaweedfs-volume-plugin

## Building the plugin

    make builder # if cross platform buildx builder is not available 
    make 
    make push 
    make distclean


## Installing the plugin

amd64:

    docker plugin install --alias seaweedfs angzam78/seaweedfs-volume-plugin:latest-amd64

arm64:

    docker plugin install --alias seaweedfs angzam78/seaweedfs-volume-plugin:latest-arm64

## Using the plugin

An example with options:

    docker volume create -d seaweedfs -o host=seaweed-filer:8888 -o filerpath=/some/remote/folder -o "-nonempty" -o "-allowOthers" -o "-volumeServerAccess=filerProxy" volume_name
