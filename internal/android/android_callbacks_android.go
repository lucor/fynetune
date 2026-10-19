//go:build android

package android

/*
#include <jni.h>
*/
import "C"

//export Java_dev_lucor_fynetune_android_PlaybackService_stopPlayback
func Java_dev_lucor_fynetune_android_PlaybackService_stopPlayback(env *C.JNIEnv, cls C.jclass) {
	StopPlaybackFromService()
}
