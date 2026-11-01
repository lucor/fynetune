//go:build android

package android

/*
#include <jni.h>

static jint beginServiceCall(JNIEnv *env) {
	return (*env)->PushLocalFrame(env, 32);
}

static void endServiceCall(JNIEnv *env) {
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionDescribe(env);
		(*env)->ExceptionClear(env);
	}
	(*env)->PopLocalFrame(env, NULL);
}

static jstring newServiceString(JNIEnv *env, const jchar *text, jsize length) {
	return (*env)->NewString(env, text, length);
}

static jbyteArray newServiceBytes(JNIEnv *env, const jbyte *data, jsize length) {
	jbyteArray result = (*env)->NewByteArray(env, length);
	if (result != NULL && length != 0) (*env)->SetByteArrayRegion(env, result, 0, length, data);
	return (*env)->ExceptionCheck(env) ? NULL : result;
}

static jint setPlaybackService(JNIEnv *env, jobject context, jboolean enabled, jint state,
		jstring station, jstring description, jstring album, jbyteArray artwork) {
	jclass intentClass = (*env)->FindClass(env, "android/content/Intent");
	if (intentClass == NULL) return -1;
	jmethodID ctor = (*env)->GetMethodID(env, intentClass, "<init>", "()V");
	if (ctor == NULL) return -1;
	jobject intent = (*env)->NewObject(env, intentClass, ctor);
	if (intent == NULL) return -1;
	jstring serviceName = (*env)->NewStringUTF(env, "dev.lucor.fynetune.android.PlaybackService");
	if (serviceName == NULL) return -1;
	jmethodID setClass = (*env)->GetMethodID(env, intentClass, "setClassName",
		"(Landroid/content/Context;Ljava/lang/String;)Landroid/content/Intent;");
	if (setClass == NULL) return -1;
	jobject configured = (*env)->CallObjectMethod(env, intent, setClass, context, serviceName);
	if (configured == NULL) return -1;
	jclass contextClass = (*env)->GetObjectClass(env, context);
	if (contextClass == NULL) return -1;
	jmethodID operation;
	if (enabled) {
		jmethodID putInt = (*env)->GetMethodID(env, intentClass, "putExtra",
			"(Ljava/lang/String;I)Landroid/content/Intent;");
		if (putInt == NULL) return -1;
		jstring stateKey = (*env)->NewStringUTF(env, "state");
		if (stateKey == NULL) return -1;
		(*env)->CallObjectMethod(env, intent, putInt, stateKey, state);
		if ((*env)->ExceptionCheck(env)) return -1;
		jmethodID putString = (*env)->GetMethodID(env, intentClass, "putExtra",
			"(Ljava/lang/String;Ljava/lang/String;)Landroid/content/Intent;");
		if (putString == NULL) return -1;
		const char *keys[] = {"station", "description", "album"};
		jstring values[] = {station, description, album};
		for (int i = 0; i < 3; i++) {
			jstring key = (*env)->NewStringUTF(env, keys[i]);
			if (key == NULL) return -1;
			(*env)->CallObjectMethod(env, intent, putString, key, values[i]);
			if ((*env)->ExceptionCheck(env)) return -1;
		}
		jmethodID putBytes = (*env)->GetMethodID(env, intentClass, "putExtra",
			"(Ljava/lang/String;[B)Landroid/content/Intent;");
		if (putBytes == NULL) return -1;
		jstring artworkKey = (*env)->NewStringUTF(env, "artwork");
		if (artworkKey == NULL) return -1;
		(*env)->CallObjectMethod(env, intent, putBytes, artworkKey, artwork);
		if ((*env)->ExceptionCheck(env)) return -1;
		jclass versionClass = (*env)->FindClass(env, "android/os/Build$VERSION");
		if (versionClass == NULL) return -1;
		jfieldID sdkField = (*env)->GetStaticFieldID(env, versionClass, "SDK_INT", "I");
		if (sdkField == NULL) return -1;
		jint sdk = (*env)->GetStaticIntField(env, versionClass, sdkField);
		const char *method = sdk >= 26 ? "startForegroundService" : "startService";
		operation = (*env)->GetMethodID(env, contextClass, method,
			"(Landroid/content/Intent;)Landroid/content/ComponentName;");
		if (operation != NULL) (*env)->CallObjectMethod(env, context, operation, intent);
	} else {
		operation = (*env)->GetMethodID(env, contextClass, "stopService", "(Landroid/content/Intent;)Z");
		if (operation != NULL) (*env)->CallBooleanMethod(env, context, operation, intent);
	}
	return (*env)->ExceptionCheck(env) || operation == NULL ? -1 : 0;
}
*/
import "C"

import (
	"fmt"
	"log/slog"
	"sync/atomic"
	"unicode/utf16"
	"unsafe"

	"fyne.io/fyne/v2/driver"
	"go.lucor.dev/fynetune/internal/favicon"
	"go.lucor.dev/fynetune/internal/playback"
)

type nativeBridge struct {
	artwork     *favicon.Icon
	artworkData []byte
}

// Attach observes playback and starts or stops the Android foreground service.
func Attach(session *playback.Session) func() {
	activeSession.Store(session)
	detach := attach(session, &nativeBridge{})
	var closed atomic.Bool
	return func() {
		if !closed.CompareAndSwap(false, true) {
			return
		}
		detach()
		activeSession.CompareAndSwap(session, nil)
	}
}

func (b *nativeBridge) Update(update mediaUpdate) error {
	if b.artworkData == nil || b.artwork != update.artwork {
		data, err := artworkPNG(update.artwork)
		if err != nil {
			slog.Warn("could not prepare Android station artwork", "error", err)
			data, err = artworkPNG(nil)
			if err != nil {
				return err
			}
		}
		b.artwork, b.artworkData = update.artwork, data
	}
	return setPlaybackService(&update, b.artworkData)
}
func (*nativeBridge) Stop() error { return setPlaybackService(nil, nil) }

func setPlaybackService(update *mediaUpdate, artwork []byte) error {
	return driver.RunNative(func(context any) error {
		android, ok := context.(*driver.AndroidContext)
		if !ok {
			return fmt.Errorf("unexpected Android runtime context %T", context)
		}
		env := (*C.JNIEnv)(unsafe.Pointer(android.Env))
		if android.Ctx == 0 {
			return fmt.Errorf("Android runtime context is unavailable")
		}
		if C.beginServiceCall(env) != 0 {
			return fmt.Errorf("could not allocate Android playback service references")
		}
		defer C.endServiceCall(env)
		var value C.jboolean
		var state C.jint
		var fields [3]C.jstring
		var image C.jbyteArray
		if update != nil {
			value = 1
			state = C.jint(update.state)
			for i, text := range []string{update.station, update.description, update.album} {
				// JNI NewStringUTF expects modified UTF-8, which cannot accept
				// ordinary UTF-8 emoji. NewString accepts UTF-16 directly.
				units := utf16.Encode([]rune(text))
				var data *C.jchar
				if len(units) != 0 {
					data = (*C.jchar)(unsafe.Pointer(&units[0]))
				}
				fields[i] = C.newServiceString(env, data, C.jsize(len(units)))
				if fields[i] == 0 {
					return fmt.Errorf("could not encode Android media metadata")
				}
			}
			var pointer *C.jbyte
			if len(artwork) != 0 {
				pointer = (*C.jbyte)(unsafe.Pointer(&artwork[0]))
			}
			image = C.newServiceBytes(env, pointer, C.jsize(len(artwork)))
			if image == 0 {
				return fmt.Errorf("could not encode Android artwork")
			}
		}

		if C.setPlaybackService(env, C.jobject(android.Ctx), value, state,
			fields[0], fields[1], fields[2], image) != 0 {
			return fmt.Errorf("could not update Android playback service")
		}
		return nil
	})
}
