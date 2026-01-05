# Pasty McUploadFace

This is a silly little app I use on my home network. It allows me to do two things easily:

1. upload random text pastes (local pastebin)
2. upload files which can be downloaded on my network via QR code or direct link

The main issue solved here was what pastebin does, but I don't want to copy anything off my network.
Copying stuff between computers just needs to be easier and there's no reason to go onto
the internet for this stuff. The other issue this solves is copying files into local storage
on phones. If I have a simple, temporary fileserver (this app) on my network, I can transfer
stuff onto phones by scanning the QR codes.

# Building Container and Running

## Build the Docker Image

```
docker build -t pasty .
```

The container runs the app on TCP port 3015 inside the container.

## Running with Data Persistence

To persist both snippets and uploaded files between container restarts, use a Docker volume mounted at `/app/data`.

First, create a named volume:

```
docker volume create pasty-data
```

Then run the container with the volume mounted to `/app/data`:

```
docker run -d --name pasty -p 3015:3015 -v pasty-data:/app/data pasty
```

This setup:
- Keeps the container running in the background (`-d`)
- Maps port 3015 inside the container to 3015 on your host
- Mounts the `pasty-data` volume to `/app/data`, persisting only `snippets.json` and the `uploads/` directory
- Leaves the executable and templates in the container image, so you can update the app without losing data

The volume only contains your data (not the application code), so you can safely rebuild and restart the container with a new version while keeping all your snippets and files.

To stop and remove the container later:

```
docker stop pasty
docker rm pasty
```

The `pasty-data` volume will remain with all your data intact.

## Running without Persistence (Temporary)

To run the container without persistence (data is lost when container stops):

```
docker run --rm -it -p 3015:3015 pasty
```

## Running Locally

If you don't want to run the container, just build the go binary and run it:

```
go build -o pasty .
./pasty -host localhost -port 3015
```
