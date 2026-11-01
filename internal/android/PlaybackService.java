package dev.lucor.fynetune.android;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.media.MediaMetadata;
import android.media.session.MediaSession;
import android.media.session.PlaybackState;
import android.os.Build;
import android.os.Bundle;
import android.os.IBinder;

public final class PlaybackService extends Service {
    private static final String CHANNEL_ID = "playback";
    private static final int NOTIFICATION_ID = 1;
    private static final String ACTION_STOP = "dev.lucor.fynetune.action.STOP_PLAYBACK";
    private static final String EXTRA_SLOT_RESERVATION_SKIP_TO_PREV =
            "android.media.playback.ALWAYS_RESERVE_SPACE_FOR.ACTION_SKIP_TO_PREVIOUS";
    private static final String EXTRA_SLOT_RESERVATION_SKIP_TO_NEXT =
            "android.media.playback.ALWAYS_RESERVE_SPACE_FOR.ACTION_SKIP_TO_NEXT";

    private MediaSession mediaSession;
    private PendingIntent open;
    private PendingIntent stop;
    private int notificationIconId;
    private int stopIconId;

    public static native void stopPlayback();

    @Override public void onCreate() {
        super.onCreate();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            NotificationChannel channel = new NotificationChannel(
                    CHANNEL_ID, "Radio playback", NotificationManager.IMPORTANCE_LOW);
            getSystemService(NotificationManager.class).createNotificationChannel(channel);
        }

        stopIconId = getResources().getIdentifier("ic_stop", "drawable", getPackageName());
        if (stopIconId == 0) {
            throw new IllegalStateException("Android Stop drawable is missing from the package");
        }
        notificationIconId = getResources().getIdentifier("ic_notification", "drawable", getPackageName());
        if (notificationIconId == 0) {
            throw new IllegalStateException("Android notification drawable is missing from the package");
        }

        Intent openIntent = new Intent();
        openIntent.setClassName(this, "org.golang.app.GoNativeActivity");
        openIntent.addFlags(Intent.FLAG_ACTIVITY_CLEAR_TOP | Intent.FLAG_ACTIVITY_SINGLE_TOP);
        open = PendingIntent.getActivity(this, 0, openIntent,
                PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);
        Intent stopIntent = new Intent(this, PlaybackService.class).setAction(ACTION_STOP);
        stop = PendingIntent.getService(this, 1, stopIntent,
                PendingIntent.FLAG_IMMUTABLE | PendingIntent.FLAG_UPDATE_CURRENT);

        mediaSession = new MediaSession(this, "FyneTune");
        mediaSession.setSessionActivity(open);
        mediaSession.setFlags(MediaSession.FLAG_HANDLES_MEDIA_BUTTONS
                | MediaSession.FLAG_HANDLES_TRANSPORT_CONTROLS);
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            Bundle extras = new Bundle();
            extras.putBoolean(EXTRA_SLOT_RESERVATION_SKIP_TO_PREV, true);
            extras.putBoolean(EXTRA_SLOT_RESERVATION_SKIP_TO_NEXT, true);
            mediaSession.setExtras(extras);
        }
        mediaSession.setCallback(new MediaSession.Callback() {
            @Override public void onStop() {
                stopPlayback();
                stopSelf();
            }

            @Override public void onCustomAction(String action, Bundle extras) {
                if (ACTION_STOP.equals(action)) {
                    onStop();
                }
            }
        });
    }

    private static Bitmap bitmapExtra(Intent intent, String name) {
        byte[] data = intent.getByteArrayExtra(name);
        return data == null ? null : BitmapFactory.decodeByteArray(data, 0, data.length);
    }

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        if (intent == null || ACTION_STOP.equals(intent.getAction())) {
            if (intent != null) {
                stopPlayback();
            }
            stopSelf();
            return START_NOT_STICKY;
        }

        String station = intent.getStringExtra("station");
        String description = intent.getStringExtra("description");
        Bitmap artwork = bitmapExtra(intent, "artwork");
        mediaSession.setMetadata(new MediaMetadata.Builder()
                .putBitmap(MediaMetadata.METADATA_KEY_ART, artwork)
                .putBitmap(MediaMetadata.METADATA_KEY_DISPLAY_ICON, artwork)
                // Publish both pairs because System UI versions use different metadata keys.
                .putString(MediaMetadata.METADATA_KEY_TITLE, station)
                .putString(MediaMetadata.METADATA_KEY_ARTIST, description)
                .putString(MediaMetadata.METADATA_KEY_ALBUM, intent.getStringExtra("album"))
                .putString(MediaMetadata.METADATA_KEY_DISPLAY_TITLE, station)
                .putString(MediaMetadata.METADATA_KEY_DISPLAY_SUBTITLE, description)
                .build());
        int state = intent.getIntExtra("state", PlaybackState.STATE_CONNECTING);
        mediaSession.setPlaybackState(new PlaybackState.Builder()
                .setState(state, PlaybackState.PLAYBACK_POSITION_UNKNOWN,
                        state == PlaybackState.STATE_PLAYING ? 1 : 0)
                .setActions(PlaybackState.ACTION_STOP)
                // Apps targeting API 33+ use PlaybackState for system buttons.
                // Use the same packaged drawable as the notification action.
                .addCustomAction(ACTION_STOP, "Stop", stopIconId)
                .build());
        mediaSession.setActive(true);
        Notification.Builder notification = Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                ? new Notification.Builder(this, CHANNEL_ID)
                : new Notification.Builder(this);
        notification
                .setContentIntent(open)
                .setCategory(Notification.CATEGORY_TRANSPORT)
                .setVisibility(Notification.VISIBILITY_PUBLIC)
                .setOngoing(true)
                .setOnlyAlertOnce(true)
                .setShowWhen(false)
                .setSmallIcon(notificationIconId)
                .setLargeIcon(artwork)
                .setStyle(new Notification.MediaStyle()
                        .setMediaSession(mediaSession.getSessionToken())
                        .setShowActionsInCompactView(0));
        notification.addAction(new Notification.Action.Builder(stopIconId, "Stop", stop).build());
        Notification current = notification
                .setContentTitle(station)
                .setContentText(description)
                .build();

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(NOTIFICATION_ID, current,
                    ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK);
        } else {
            startForeground(NOTIFICATION_ID, current);
        }
        return START_NOT_STICKY;
    }

    @Override public void onDestroy() {
        mediaSession.setActive(false);
        mediaSession.release();
        stopForeground(true);
        super.onDestroy();
    }

    @Override public IBinder onBind(Intent intent) { return null; }
}
