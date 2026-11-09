/** 应用进程入口，只用来启用 Hilt。 */
package click.cowa.linbit.app

import android.app.Application
import dagger.hilt.android.HiltAndroidApp

@HiltAndroidApp
class LinbitApplication : Application()
