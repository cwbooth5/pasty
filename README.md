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

To build the docker container, just do this

```
docker build -t pasty .
```

The container's going to have the app running on TCP port 8090 inside the container and you can map
that internal port to whatever you want externally when you run the container.

```
docker run --rm -it -p 8090:9000 pasty
```

If you don't want to run the container, just build the go binary and run it.
