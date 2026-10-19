//go:build android

package android

/*
#include <jni.h>

static jint setPlaybackService(JNIEnv *env, jobject context, jboolean enabled) {
	jclass intentClass = (*env)->FindClass(env, "android/content/Intent");
	if (intentClass == NULL) return -1;
	jmethodID ctor = (*env)->GetMethodID(env, intentClass, "<init>", "()V");
	if (ctor == NULL) return -1;
	jobject intent = (*env)->NewObject(env, intentClass, ctor);
	if (intent == NULL) return -1;
	jstring serviceName = (*env)->NewStringUTF(env, "dev.lucor.fynetune.android.PlaybackService");
	jmethodID setClass = (*env)->GetMethodID(env, intentClass, "setClassName",
		"(Landroid/content/Context;Ljava/lang/String;)Landroid/content/Intent;");
	if (setClass == NULL) return -1;
	jobject configured = (*env)->CallObjectMethod(env, intent, setClass, context, serviceName);
	if (configured == NULL) return -1;
	jclass contextClass = (*env)->GetObjectClass(env, context);
	jmethodID operation;
	jobject result = NULL;
	if (enabled) {
		jclass versionClass = (*env)->FindClass(env, "android/os/Build$VERSION");
		jfieldID sdkField = (*env)->GetStaticFieldID(env, versionClass, "SDK_INT", "I");
		jint sdk = (*env)->GetStaticIntField(env, versionClass, sdkField);
		const char *method = sdk >= 26 ? "startForegroundService" : "startService";
		operation = (*env)->GetMethodID(env, contextClass, method,
			"(Landroid/content/Intent;)Landroid/content/ComponentName;");
		if (operation != NULL) result = (*env)->CallObjectMethod(env, context, operation, intent);
	} else {
		operation = (*env)->GetMethodID(env, contextClass, "stopService", "(Landroid/content/Intent;)Z");
		if (operation != NULL) (*env)->CallBooleanMethod(env, context, operation, intent);
	}
	int failed = (*env)->ExceptionCheck(env);
	if (failed) {
		(*env)->ExceptionDescribe(env);
		(*env)->ExceptionClear(env);
	}
	if (result != NULL) (*env)->DeleteLocalRef(env, result);
	(*env)->DeleteLocalRef(env, contextClass);
	(*env)->DeleteLocalRef(env, configured);
	(*env)->DeleteLocalRef(env, serviceName);
	(*env)->DeleteLocalRef(env, intent);
	(*env)->DeleteLocalRef(env, intentClass);
	return failed || operation == NULL ? -1 : 0;
}
*/
import "C"

import (
	"fmt"
	"sync/atomic"
	"unsafe"

	"fyne.io/fyne/v2/driver"
	"go.lucor.dev/fynetune/internal/playback"
)

type nativeBridge struct{}

// Attach observes playback and starts or stops the Android foreground service.
func Attach(session *playback.Session) func() {
	activeSession.Store(session)
	detach := attach(session, nativeBridge{})
	var closed atomic.Bool
	return func() {
		if !closed.CompareAndSwap(false, true) {
			return
		}
		detach()
		activeSession.CompareAndSwap(session, nil)
	}
}

func (nativeBridge) Start() error { return setPlaybackService(true) }
func (nativeBridge) Stop() error  { return setPlaybackService(false) }

func setPlaybackService(enabled bool) error {
	return driver.RunNative(func(context any) error {
		android, ok := context.(*driver.AndroidContext)
		if !ok {
			return fmt.Errorf("unexpected Android runtime context %T", context)
		}
		var value C.jboolean
		if enabled {
			value = 1
		}
		if C.setPlaybackService((*C.JNIEnv)(unsafe.Pointer(android.Env)), C.jobject(android.Ctx), value) != 0 {
			return fmt.Errorf("could not update Android playback service")
		}
		return nil
	})
}
