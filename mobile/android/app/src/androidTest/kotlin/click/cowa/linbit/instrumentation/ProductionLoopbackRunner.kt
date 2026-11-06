package click.cowa.linbit.instrumentation

import android.app.Application
import android.content.Context
import androidx.test.runner.AndroidJUnitRunner

/** Instrumented evidence never boots the production Hilt application/persisted session. */
class ProductionLoopbackRunner : AndroidJUnitRunner() {
    override fun newApplication(cl: ClassLoader?, className: String?, context: Context?): Application = newApplication(cl, InstrumentationEvidenceApplication::class.java.name, context)
}

/** Empty application: no production network bootstrap, credentials, remote money service. */
class InstrumentationEvidenceApplication : Application()
