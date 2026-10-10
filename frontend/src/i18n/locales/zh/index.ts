import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import batchImage from './batchImage'
import imageWorkbench from './imageWorkbench'
import admin from './admin'
import misc from './misc'

export default {
  ...landing,
  ...common,
  ...dashboard,
  ...batchImage,
  ...imageWorkbench,
  admin,
  ...misc,
}
